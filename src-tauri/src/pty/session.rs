// PtySession：单个嵌入式终端会话的运行时状态。
//
// 生命周期（docs/embedded_terminal.md §3.4）：spawn 存 store → reader 线程推输出 →
// shell 退出置 exited（会话保留 store 供前端重开）→ shutdown 时 kill + 移出 store。
//
// 线程安全设计：writer/child 都被 Mutex 包裹——take_writer 只能调一次故 writer 需要跨线程
// 共享；child 的 kill（shutdown 时）与进程自然退出并发。master 的 resize 是 &self 内部同步，
// 无需额外锁。SessionIo 是输出侧共享内核（Arc）：reader 线程持它推流，reattach 命令经 store
// 短锁替换 listener，二者解耦——reader 永不持有 store 锁，高频输出不阻塞命令层。
// ring buffer（scrollback 重载用）在任务 3 接入 SessionIo。

use std::io::{Read, Write};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

use portable_pty::{Child, MasterPty};
use serde::{Deserialize, Serialize};
use specta::Type;
use tauri::ipc::Channel;

/// PTY → 前端 的事件（单 Channel 双分支，数据与退出同序到达）。
/// 不出现在命令签名（仅作 Channel<PtyEvent> 泛型载荷），需在 lib.rs 用 .typ::<PtyEvent>() 注册。
#[derive(Clone, Debug, Serialize, Deserialize, Type)]
#[serde(rename_all = "camelCase", tag = "kind")]
pub enum PtyEvent {
    /// 终端输出（UTF-8 边界切分后的完整字符串块）。
    Data { data: String },
    /// shell 已退出（正常 exit / 被 kill）。会话仍在 store，前端可重开（重新 spawn）。
    Exit,
}

/// 会话输出侧共享内核。reader 线程与命令层（reattach 换 listener）各持 Arc。
pub struct SessionIo {
    /// 当前输出订阅者（webview 传来的 Channel）。None = 无订阅（webview 已断开/未挂载），
    /// 输出静默丢弃。Mutex 短临界区：reader 每块输出 lock 一次取快照。
    pub listener: Mutex<Option<Channel<PtyEvent>>>,
    /// shell 退出标志（reader EOF 置位）。PtySession.exited 语义合并至此，
    /// reader 线程与命令层（list/重开判定）共享同一事实源。
    pub exited: AtomicBool,
}

impl SessionIo {
    pub fn new() -> Self {
        Self {
            listener: Mutex::new(None),
            exited: AtomicBool::new(false),
        }
    }

    /// 替换 listener（spawn/reattach 装载）。返回旧值供调用方感知替换。
    pub fn set_listener(&self, channel: Channel<PtyEvent>) -> Option<Channel<PtyEvent>> {
        self.listener
            .lock()
            .expect("SessionIo listener mutex poisoned")
            .replace(channel)
    }

    /// 推送事件：无订阅者或 send 失败（旧 webview 已销毁）均静默丢弃。
    fn emit(&self, event: PtyEvent) {
        let snapshot = self
            .listener
            .lock()
            .expect("SessionIo listener mutex poisoned")
            .clone();
        if let Some(channel) = snapshot {
            let _ = channel.send(event);
        }
    }
}

/// UTF-8 尾部缓冲：PTY 是字节流，多字节 UTF-8 字符（中文）可能跨读块边界被切断。
/// 直接 from_utf8_lossy 会瞬间乱码；保留不完整尾部等下一块拼接，只送出完整前缀。
struct Utf8Tail {
    pending: Vec<u8>,
}

impl Utf8Tail {
    fn new() -> Self {
        Self {
            pending: Vec::new(),
        }
    }

    /// 拼接新块并切出所有完整 UTF-8 序列；不完整尾部留待下块。
    /// 非法字节序列（客户端发送二进制/编码错误）丢弃尾部字节，避免永久卡死。
    fn take_complete(&mut self, chunk: &[u8]) -> String {
        self.pending.extend_from_slice(chunk);
        match std::str::from_utf8(&self.pending) {
            Ok(s) => {
                let complete = s.to_string();
                self.pending.clear();
                complete
            }
            Err(e) => {
                let valid = e.valid_up_to();
                let complete = String::from_utf8_lossy(&self.pending[..valid]).into_owned();
                // error_len：Some(n) = 确定非法字节，丢弃之；None = 序列不完整，保留尾部。
                match e.error_len() {
                    Some(bad) => {
                        self.pending.drain(..valid + bad);
                    }
                    None => {
                        self.pending.drain(..valid);
                    }
                }
                complete
            }
        }
    }
}

/// reader 线程：阻塞读 PTY 输出 → UTF-8 切分 → 推 listener；EOF/Err → 置 exited + Exit 事件。
/// spawn 时启动，持 Arc<SessionIo> 与 cloned reader，不碰 store（会话移除后仍会读到 EOF 自然退出）。
pub fn spawn_reader_thread(mut reader: Box<dyn Read + Send>, io: Arc<SessionIo>) {
    std::thread::spawn(move || {
        let mut tail = Utf8Tail::new();
        let mut buf = [0u8; 8192];
        loop {
            match reader.read(&mut buf) {
                Ok(0) => break, // EOF：shell 退出
                Ok(n) => {
                    let text = tail.take_complete(&buf[..n]);
                    if !text.is_empty() {
                        io.emit(PtyEvent::Data { data: text });
                    }
                }
                Err(_) => break, // PTY 关闭（kill 后）
            }
        }
        io.exited.store(true, Ordering::SeqCst);
        io.emit(PtyEvent::Exit);
    });
}

/// 单个 PTY 会话。key 为 issueId，存于 PtySessionStore。
pub struct PtySession {
    /// 会话锚点 = issue uuid。
    pub issue_id: String,
    /// 工作目录（前端派生的 `${workspace_base_dir}/${issueId}`）。
    pub cwd: String,
    /// PTY master 端：resize。
    pub master: Box<dyn MasterPty>,
    /// PTY 写入端（take_writer 仅一次，故 Mutex 共享给 pty_write 命令线程）。
    pub writer: Mutex<Box<dyn Write + Send>>,
    /// shell 子进程（kill）。
    pub child: Mutex<Box<dyn Child + Send>>,
    /// 输出侧共享内核（reader 线程同持一份 Arc）。
    pub io: Arc<SessionIo>,
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
        io: Arc<SessionIo>,
        started_at: i64,
    ) -> Self {
        Self {
            issue_id,
            cwd,
            master,
            writer: Mutex::new(writer),
            child: Mutex::new(child),
            io,
            started_at,
        }
    }

    /// 会话是否已退出（shell 退出/被 kill 后置位；会话仍留 store 供前端重开）。
    pub fn exited(&self) -> bool {
        self.io.exited.load(Ordering::SeqCst)
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

    /// 关闭会话：kill shell（kill 后 reader 读到 EOF 自然收尾并置 exited + Exit 事件）。
    /// 不从 store 移除——移除由调用方（provider/state 层）统一处理。
    pub fn shutdown(&self) -> Result<(), String> {
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

#[cfg(test)]
mod tests {
    use super::*;

    /// UTF-8 边界切分：中文 3 字节序列被任意切断后拼接无损，非法字节被丢弃。
    #[test]
    fn utf8_tail_splits_on_boundary() {
        let mut tail = Utf8Tail::new();
        // "你好" = e4 bd a0 | e5 a5 bd（两个 3 字节序列），故意切成 1+2+3 字节三块
        let bytes = "你好".as_bytes();
        assert_eq!(tail.take_complete(&bytes[..1]), ""); // 仅 e4（"你"第 1 字节）不完整
        assert_eq!(tail.take_complete(&bytes[1..3]), "你"); // e4+bd+a0 补全"你"；e5 a5 bd 未到
        assert_eq!(tail.take_complete(&bytes[3..]), "好"); // e5 a5 bd 补全"好"

        // 非法字节（0xFF）被丢弃；其后合法 ASCII 因在非法点之后暂留 pending，下一块送出。
        assert_eq!(tail.take_complete(&[0xFF, b'o', b'k']), "");
        assert_eq!(tail.take_complete(b"!\n"), "ok!\n");
        // 尾部不完整序列（"好" = e5 a5 bd 的前 2 字节）挂起等待下一块
        let hao = "好".as_bytes();
        assert_eq!(tail.take_complete(&hao[..2]), "");
        assert_eq!(tail.take_complete(&hao[2..]), "好");
    }
}
