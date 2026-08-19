// pty 域：嵌入式终端会话生命周期管理（docs/embedded_terminal.md）。
//
// 与 terminal/ 域的边界：terminal/ 负责跳转/打开外部终端（iTerm2/Terminal.app），
// 本域负责应用内 PTY 会话（spawn/写/resize/关闭/reattach）——一 issue 一终端，
// 锚点为 issue uuid，cwd 为 `${workspace_base_dir}/${issueId}`。
//
// 子模块：
//   provider        —— PtyProvider trait（远程 SSH 扩展预留）+ SpawnOpts/PtySpawned/PtySessionInfo
//   local_provider  —— LocalPtyProvider（portable-pty 本机实现，spawn 即起 reader 线程）
//   session         —— PtySession + SessionIo（输出共享内核：listener Channel + exited）
//   state           —— PtySessionStore（Mutex<HashMap>，抗 webview 刷新常驻）
//
// 输出通道：pty_spawn 传 Channel<PtyEvent>（Data/Exit 单通道双分支，tauri-specta rc.25
// 原生支持，已 spike 验证）。emit 备选（EVENT_PTY_*）未采用。
// reattach 命令（exists/reattach + ring replay）在任务 3 接入。

pub mod local_provider;
pub mod provider;
pub mod session;
pub mod shell_ready;
pub mod state;

use local_provider::LocalPtyProvider;
use provider::{PtyProvider, PtyReattached, PtySessionInfo, PtySpawned, SpawnOpts};
use session::PtyEvent;
use state::PtySessionStore;

/// 全局 provider 实例。Tauri State 管理的是 store，provider 以 once 语义全局唯一
///（本机后端无状态，仅持 store 引用不便拆双份——直接 lazy 常量）。
fn provider() -> &'static LocalPtyProvider {
    static PROVIDER: std::sync::OnceLock<LocalPtyProvider> = std::sync::OnceLock::new();
    PROVIDER.get_or_init(LocalPtyProvider::new)
}

/// 启动/复用会话（幂等）：未退出复用 + 换装 listener；已退出重起（重开语义）。
/// 前端挂载即调本命令；输出/退出事件经 on_event Channel 流式回传。
#[tauri::command]
#[specta::specta]
pub fn pty_spawn(
    opts: SpawnOpts,
    on_event: tauri::ipc::Channel<PtyEvent>,
) -> Result<PtySpawned, String> {
    let result = provider().spawn(opts, on_event);
    match &result {
        Ok(s) => log::info!("[pty] spawn ok issue_id={} fresh={} scrollback={}", s.issue_id, s.fresh, s.scrollback.len()),
        Err(e) => log::warn!("[pty] spawn failed: {}", e),
    }
    result
}

/// 键盘输入写入会话。
#[tauri::command]
#[specta::specta]
pub fn pty_write(issue_id: String, data: String) -> Result<(), String> {
    provider().write(&issue_id, data.as_bytes())
}

/// 终端尺寸变化（xterm onResize）。
#[tauri::command]
#[specta::specta]
pub fn pty_resize(issue_id: String, cols: u16, rows: u16) -> Result<(), String> {
    provider().resize(&issue_id, cols, rows)
}

/// 关闭单个会话（kill shell + 移出 store）。
#[tauri::command]
#[specta::specta]
pub fn pty_shutdown(issue_id: String) -> Result<(), String> {
    provider().shutdown(&issue_id)
}

/// 列出全部会话快照（调试/后续状态栏用）。
#[tauri::command]
#[specta::specta]
pub fn pty_list_sessions() -> Vec<PtySessionInfo> {
    provider().list()
}

/// 会话是否存在（含已退出）。前端挂载顺序：exists → 存在则 reattach，不存在才 spawn。
#[tauri::command]
#[specta::specta]
pub fn pty_exists(issue_id: String) -> bool {
    provider().exists(&issue_id)
}

/// 重挂会话（webview 刷新/切换 issue 回切）：ring 快照随返回值送达 + 换装 listener 续流。
/// 已退出会话照常返回（exited=true）；不存在返回 None（前端转 pty_spawn）。
#[tauri::command]
#[specta::specta]
pub fn pty_reattach(
    issue_id: String,
    on_event: tauri::ipc::Channel<PtyEvent>,
) -> Result<Option<PtyReattached>, String> {
    let result = provider().reattach(&issue_id, on_event);
    if let Ok(Some(r)) = &result {
        log::info!("[pty] reattach issue_id={} exited={} scrollback={}", issue_id, r.exited, r.scrollback.len());
    }
    result
}

/// 应用退出时（RunEvent::Exit）调用：遍历 store kill 全部 shell 并清空。
/// reader 线程在 kill 后读到 EOF 自然退出，无需 join。
pub fn shutdown_all(store: &PtySessionStore) {
    let mut map = store
        .0
        .lock()
        .expect("PtySessionStore mutex poisoned");
    let sessions: Vec<_> = map.drain().collect();
    for (_, session) in sessions {
        let issue_id = session.issue_id.clone();
        if let Err(e) = session.shutdown() {
            log::warn!(
                "[pty] shutdown_all kill {} failed: {}",
                issue_id,
                e
            );
        }
    }
    log::info!("[pty] shutdown_all complete");
}
