// PtyProvider：PTY 后端抽象（嵌入式终端，见 docs/embedded_terminal.md §3.1）。
//
// 本期只实现 LocalPtyProvider（本机 portable-pty）。trait 保留远程终端（SSH relay）
// 的扩展位：命令层与前端组件只依赖本抽象，将来加 provider 时上层不动。
//
// 输出与退出事件不经 trait 方法返回，由实现侧通过 Channel/emit 回传前端（任务 2 接线）。

use serde::{Deserialize, Serialize};
use specta::Type;
use specta_typescript::Number;
use tauri::ipc::Channel;

use super::session::{PtyEvent, PtySession};

/// spawn 入参。cwd 由前端派生（`${workspace_base_dir}/${issueId}`），
/// 目录不存在时本模块不创建（skills 集成职责），spawn 失败自然暴露。
#[derive(Clone, Debug, Serialize, Deserialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct SpawnOpts {
    /// 会话锚点（store key）：main pane = issue uuid，附加 pane = `issueId::paneId`
    ///（split 分割窗口，terminal_02 §3.1）。
    pub session_id: String,
    /// 工作目录绝对路径。
    pub cwd: String,
    /// 初始列数（前端 xterm addon-fit 实测值）。
    pub cols: u16,
    /// 初始行数。
    pub rows: u16,
    /// 启动注入命令（如 "claude"）：fresh spawn 且 shell 为 zsh/bash 时走包装
    /// spawn（shell-ready barrier 精确锚定提示符就绪后注入）；其余 shell 走
    /// fast 注入降级。None = 现状裸 spawn。reattach/复用分支不重注入。
    #[serde(default)]
    pub startup_command: Option<String>,
    /// 直接 spawn 命令（claude_orca T5.1，chat 模式 CLI 直启）：整串命令，首
    /// token 为 CLI 名（如 "claude"，T5.2 的 "claude --resume <id>" 同形）。
    /// 优先级高于 startup_command：在场时无 shell 中转、无 shell-ready barrier
    /// ——PTY 直接 exec CLI（T1.4 归因 env 打标照常注入），CLI 退出即 pane
    /// 退出（无 shell 回落，走 exited UI；跑普通命令用附加 pane）。CLI 路径
    /// 经 login shell 探测解析，失败回落 startup_command 注入路径（warn log）。
    #[serde(default)]
    pub direct_command: Option<String>,
}

/// 会话信息快照（pty_list_sessions 返回，调试/后续状态栏用）。
#[derive(Clone, Debug, Serialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct PtySessionInfo {
    /// 会话锚点（store key，见 SpawnOpts.session_id）。
    pub session_id: String,
    /// 会话工作目录。
    pub cwd: String,
    /// shell 进程 pid（拿不到为 0）。
    pub pid: u32,
    /// 会话是否已退出（shell 退出/被 kill 后置位；会话仍留 store 供前端重开）。
    pub exited: bool,
    /// spawn 时间（毫秒时间戳，远小于 2^53，精度安全）。
    #[specta(type = Number)]
    pub started_at: i64,
}

/// pty_spawn 成功荷载：会话元信息，前端挂载时直接渲染（无需再查 list）。
#[derive(Clone, Debug, Serialize, Deserialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct PtySpawned {
    /// 会话锚点（store key，见 SpawnOpts.session_id）。
    pub session_id: String,
    /// 会话工作目录。
    pub cwd: String,
    /// shell 进程 pid（拿不到为 0）。
    pub pid: u32,
    /// spawn 时间（毫秒时间戳）。
    #[specta(type = Number)]
    pub started_at: i64,
    /// 本次是否新起 shell（false = 复用现有会话，如 webview 刷新后重挂）。
    pub fresh: bool,
    /// 复用会话时的 ring 快照（StrictMode 双挂载/快速重挂场景回放早期输出）；
    /// 全新 spawn 恒为空串。
    pub scrollback: String,
}

/// pty_reattach 成功荷载：scrollback 随返回值一次性送达，实时流走已换装的 Channel。
#[derive(Clone, Debug, Serialize, Deserialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct PtyReattached {
    /// 会话锚点（store key，见 SpawnOpts.session_id）。
    pub session_id: String,
    /// 会话是否已退出（true = 前端直接展示终态 + 重开按钮，不期待实时流）。
    pub exited: bool,
    /// ring buffer 全量拼接（scrollback 重载，已按 UTF-8 边界切分保证合法）。
    pub scrollback: String,
}

/// PTY 后端抽象。本期仅 LocalPtyProvider；远程 provider（SSH）为后续扩展预留。
pub trait PtyProvider: Send + Sync {
    /// 启动会话（幂等）：未退出会话复用并换装 listener；已退出会话移除重起（重开语义）。
    /// 返回会话元信息。
    fn spawn(&self, opts: SpawnOpts, listener: Channel<PtyEvent>) -> Result<PtySpawned, String>;
    /// 键盘输入写入会话。
    fn write(&self, id: &str, data: &[u8]) -> Result<(), String>;
    /// 终端尺寸变化（xterm onResize）。
    fn resize(&self, id: &str, cols: u16, rows: u16) -> Result<(), String>;
    /// 关闭单个会话（kill shell + 移出 store）。
    fn shutdown(&self, id: &str) -> Result<(), String>;
    /// 关闭整个 issue 的全部 pane 会话（key 以 `issueId::` 为前缀），
    /// 返回关闭数。模块 2 split 后一 issue 多 pane；issue 删除时联动调用。
    fn shutdown_issue(&self, issue_id: &str) -> Result<usize, String>;
    /// 会话是否存在（含已退出；前端挂载时探测，存在则 reattach，不存在才 spawn）。
    fn exists(&self, id: &str) -> bool;
    /// 重挂会话：ring 快照随返回值送达 + 换装 listener 从快照点续流。
    /// 已退出会话照常返回（exited=true + 退出前 scrollback）；不存在返回 None。
    fn reattach(
        &self,
        id: &str,
        listener: Channel<PtyEvent>,
    ) -> Result<Option<PtyReattached>, String>;
    /// 列出全部会话快照。
    fn list(&self) -> Vec<PtySessionInfo>;
    /// 从 store 取会话引用（reattach/list 内部用，不出命令边界）。
    fn with_session<T>(
        &self,
        id: &str,
        f: &mut dyn FnMut(&mut PtySession) -> T,
    ) -> Result<T, String>;
}
