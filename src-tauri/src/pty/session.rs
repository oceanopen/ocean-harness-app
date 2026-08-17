// PtySession：单个嵌入式终端会话的运行时状态。
//
// 生命周期（docs/embedded_terminal.md §3.4）：spawn 存 store → reader 线程推输出 →
// shell 退出置 exited（会话保留 store 供前端重开）→ shutdown 时 kill + 移出 store。
//
// 线程安全设计：writer/child 都被 Mutex 包裹——take_writer 只能调一次故 writer 需要跨线程
// 共享；child 的 wait（reader 收尾时）与 kill（shutdown 时）可能并发。master 的 resize 是
// &self 内部同步，无需额外锁。ring buffer（scrollback 重载用）在任务 3 接入。

use std::io::Write;
use std::sync::Mutex;
use std::sync::atomic::AtomicBool;

use portable_pty::{Child, MasterPty};

/// 单个 PTY 会话。key 为 issueId，存于 PtySessionStore。
pub struct PtySession {
    /// 会话锚点 = issue uuid。
    pub issue_id: String,
    /// 工作目录（前端派生的 `${workspace_base_dir}/${issueId}`）。
    pub cwd: String,
    /// PTY master 端：resize / 后续 ring reader 克隆。
    pub master: Box<dyn MasterPty>,
    /// PTY 写入端（take_writer 仅一次，故 Mutex 共享给 pty_write 命令线程）。
    pub writer: Mutex<Box<dyn Write + Send>>,
    /// shell 子进程（wait / kill）。
    pub child: Mutex<Box<dyn Child + Send>>,
    /// 退出标志：置位后会话仍留 store（前端重开 = 移除旧会话重新 spawn）。
    pub exited: AtomicBool,
    /// spawn 时间（毫秒时间戳）。
    pub started_at: i64,
}

impl PtySession {
    pub fn new(
        issue_id: String,
        cwd: String,
        master: Box<dyn MasterPty>,
        writer: Box<dyn Write + Send>,
        child: Box<dyn Child + Send>,
        started_at: i64,
    ) -> Self {
        Self {
            issue_id,
            cwd,
            master,
            writer: Mutex::new(writer),
            child: Mutex::new(child),
            exited: AtomicBool::new(false),
            started_at,
        }
    }

    /// 写入键盘输入。锁 poison 视为会话已坏，返回 Err 让前端感知。
    pub fn write_input(&self, data: &[u8]) -> Result<(), String> {
        let mut writer = self.writer.lock().map_err(|_| {
            format!(
                "pty session {} writer lock poisoned",
                self.issue_id
            )
        })?;
        writer
            .write_all(data)
            .map_err(|e| format!("pty write failed: {e}"))
    }

    /// 调整终端尺寸（内核 winsize + SIGWINCH 通知 shell）。
    pub fn resize(&self, cols: u16, rows: u16) -> Result<(), String> {
        self.master
            .resize(portable_pty::PtySize {
                rows,
                cols,
                pixel_width: 0,
                pixel_height: 0,
            })
            .map_err(|e| format!("pty resize failed: {e}"))
    }

    /// 关闭会话：kill shell（kill 后 reader 读到 EOF 自然收尾）+ 置退出标志。
    /// 不从 store 移除——移除由调用方（provider/state 层）统一处理。
    pub fn shutdown(&self) -> Result<(), String> {
        self.exited
            .store(true, std::sync::atomic::Ordering::SeqCst);
        let mut child = self.child.lock().map_err(|_| {
            format!(
                "pty session {} child lock poisoned",
                self.issue_id
            )
        })?;
        child
            .kill()
            .map_err(|e| format!("pty kill failed: {e}"))
    }
}
