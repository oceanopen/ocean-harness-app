// PtyProvider：PTY 后端抽象（嵌入式终端，见 docs/embedded_terminal.md §3.1）。
//
// 本期只实现 LocalPtyProvider（本机 portable-pty）。trait 保留远程终端（SSH relay）
// 的扩展位：命令层与前端组件只依赖本抽象，将来加 provider 时上层不动。
//
// 输出与退出事件不经 trait 方法返回，由实现侧通过 Channel/emit 回传前端（任务 2 接线）。

use serde::{Deserialize, Serialize};
use specta::Type;
use specta_typescript::Number;

use super::session::PtySession;

/// spawn 入参。cwd 由前端派生（`${workspace_base_dir}/${issueId}`），
/// 目录不存在时本模块不创建（skills 集成职责），spawn 失败自然暴露。
#[derive(Clone, Debug, Serialize, Deserialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct SpawnOpts {
    /// 会话锚点 = issue uuid（一 issue 一终端，sessionId 即 issueId）。
    pub issue_id: String,
    /// 工作目录绝对路径。
    pub cwd: String,
    /// 初始列数（前端 xterm addon-fit 实测值）。
    pub cols: u16,
    /// 初始行数。
    pub rows: u16,
}

/// 会话信息快照（pty_list_sessions 返回，调试/后续状态栏用）。
#[derive(Clone, Debug, Serialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct PtySessionInfo {
    /// 会话锚点 = issue uuid。
    pub issue_id: String,
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

/// PTY 后端抽象。本期仅 LocalPtyProvider；远程 provider（SSH）为后续扩展预留。
pub trait PtyProvider: Send + Sync {
    /// 启动会话，返回 sessionId（= issueId）。同 issueId 已有会话时直接返回现有（幂等）。
    fn spawn(&self, opts: SpawnOpts) -> Result<String, String>;
    /// 键盘输入写入会话。
    fn write(&self, id: &str, data: &[u8]) -> Result<(), String>;
    /// 终端尺寸变化（xterm onResize）。
    fn resize(&self, id: &str, cols: u16, rows: u16) -> Result<(), String>;
    /// 关闭单个会话（kill shell + 移出 store）。
    fn shutdown(&self, id: &str) -> Result<(), String>;
    /// 列出全部会话快照。
    fn list(&self) -> Vec<PtySessionInfo>;
    /// 从 store 取会话引用（reattach/list 内部用，不出命令边界）。
    fn with_session<T>(
        &self,
        id: &str,
        f: &mut dyn FnMut(&mut PtySession) -> T,
    ) -> Result<T, String>;
}
