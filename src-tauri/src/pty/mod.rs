// pty 域：嵌入式终端会话生命周期管理（docs/embedded_terminal.md）。
//
// 与 terminal/ 域的边界：terminal/ 负责跳转/打开外部终端（iTerm2/Terminal.app），
// 本域负责应用内 PTY 会话（spawn/写/resize/关闭/reattach）——会话锚点（store key）
// 统一为 `issueId::<paneId>`（main → `issueId::main`，split → `issueId::<uuid>`），
// cwd 为 `${workspace_base_dir}/${issueId}`（同一 issue 的全部 pane 同目录）。
//
// 子模块：
//   claude_state    —— claude 运行态探测（进程树父链匹配，按钮置灰驱动）
//   cli_bin         —— CLI 直启路径探测（login shell which + PATH harvest，T5.1）
//   local_provider  —— LocalPtyProvider（portable-pty 本机实现，spawn 即起 reader 线程）
//   provider        —— PtyProvider trait（远程 SSH 扩展预留）+ SpawnOpts/PtySpawned/PtySessionInfo
//   session         —— PtySession + SessionIo（输出共享内核：listener Channel + exited）
//   state           —— PtySessionStore（Mutex<HashMap>，抗 webview 刷新常驻）
//
// 输出通道：pty_spawn 传 Channel<PtyEvent>（Data/Exit 单通道双分支，tauri-specta rc.25
// 原生支持，已 spike 验证）。emit 备选（EVENT_PTY_*）未采用。
// reattach 命令（exists/reattach + ring replay）在任务 3 接入。
// （chat 模式退役：shell_ready 注入中间层已删，spawn 只剩裸 shell 与 direct 两条路径）

pub mod claude_state;
pub mod cli_bin;
pub mod local_provider;
pub mod provider;
pub mod session;
pub mod state;

use local_provider::LocalPtyProvider;
use provider::{PtyProvider, PtyReattached, PtySessionInfo, PtySpawned, SpawnOpts};
use session::PtyEvent;
use tauri::Manager;
use state::PtySessionStore;

/// 全局 provider 实例。Tauri State 管理的是 store，provider 以 once 语义全局唯一
///（本机后端无状态，仅持 store 引用不便拆双份——直接 lazy 常量）。
fn provider() -> &'static LocalPtyProvider {
    static PROVIDER: std::sync::OnceLock<LocalPtyProvider> = std::sync::OnceLock::new();
    PROVIDER.get_or_init(LocalPtyProvider::new)
}

/// 启动/复用会话（幂等）：未退出复用 + 换装 listener；已退出重起（重开语义）。
/// 前端挂载即调本命令；输出/退出事件经 on_event Channel 流式回传。
///
/// 业务 env 注入（仅真正 spawn 新进程时）：WE_TERMINAL_PORT 指向本应用 Go sidecar 的
/// HTTP 端口（HttpServerState 持有，默认 dev=9000/build=9100，可被用户设置覆盖——
/// 服务未启动时也是有效回退值）。ocean-harness 插件捆绑的 .mcp.json 以
/// ${WE_TERMINAL_PORT:-9100} 展开其 MCP 端点 url，嵌入式终端内的 claude 会话据此连上
/// 本进程的 /mcp/streamableHttp/weTerminal。
#[tauri::command]
#[specta::specta]
pub fn pty_spawn(
    app: tauri::AppHandle,
    opts: SpawnOpts,
    on_event: tauri::ipc::Channel<PtyEvent>,
) -> Result<PtySpawned, String> {
    let http_port = app
        .state::<crate::shared::http_server::HttpServerState>()
        .port
        .load(std::sync::atomic::Ordering::SeqCst);
    let envs = vec![("WE_TERMINAL_PORT".to_string(), http_port.to_string())];
    let result = provider().spawn(opts, on_event, &envs);
    match &result {
        Ok(s) => log::info!(
            "[pty] spawn ok session_id={} fresh={} scrollback={}",
            s.session_id,
            s.fresh,
            s.scrollback.len()
        ),
        Err(e) => log::warn!("[pty] spawn failed: {}", e),
    }
    result
}

/// 键盘输入写入会话。
#[tauri::command]
#[specta::specta]
pub fn pty_write(session_id: String, data: String) -> Result<(), String> {
    provider().write(&session_id, data.as_bytes())
}

/// 终端尺寸变化（xterm onResize）。
#[tauri::command]
#[specta::specta]
pub fn pty_resize(session_id: String, cols: u16, rows: u16) -> Result<(), String> {
    provider().resize(&session_id, cols, rows)
}

/// 关闭单个会话（kill shell + 移出 store）。
#[tauri::command]
#[specta::specta]
pub fn pty_shutdown(session_id: String) -> Result<(), String> {
    provider().shutdown(&session_id)
}

/// 关闭整个 issue 的全部 pane 会话（key 以 `issueId::` 为前缀）。
/// issue 删除联动调用（模块 2 split 后一 issue 多 pane，防孤儿会话）。
#[tauri::command]
#[specta::specta]
pub fn pty_shutdown_issue(issue_id: String) -> Result<(), String> {
    provider().shutdown_issue(&issue_id).map(|_| ())
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
pub fn pty_exists(session_id: String) -> bool {
    provider().exists(&session_id)
}

/// 本会话 shell 子进程树内是否跑着 claude（terminal_03 §3.2 按钮置灰驱动）。
/// 进程树匹配（claude pid 沿父链找本会话 shell pid），精确到具体终端；
/// 前端事件 + 轮询混合驱动（useClaudeRunning）。
/// 注意：查询必须走 provider() 自持的 store（spawn 写入侧同一实例）——曾因
/// app.manage 出另一恒空实例致 probe 恒 false（幽灵 manage 已删，见 state.rs）。
#[tauri::command]
#[specta::specta]
pub fn pty_claude_running(session_id: String) -> bool {
    claude_state::claude_running(provider().store(), &session_id)
}

/// 重挂会话（webview 刷新/切换 issue 回切）：ring 快照随返回值送达 + 换装 listener 续流。
/// 已退出会话照常返回（exited=true）；不存在返回 None（前端转 pty_spawn）。
#[tauri::command]
#[specta::specta]
pub fn pty_reattach(
    session_id: String,
    on_event: tauri::ipc::Channel<PtyEvent>,
) -> Result<Option<PtyReattached>, String> {
    let result = provider().reattach(&session_id, on_event);
    if let Ok(Some(r)) = &result {
        log::info!(
            "[pty] reattach session_id={} exited={} scrollback={}",
            session_id,
            r.exited,
            r.scrollback.len()
        );
    }
    result
}

/// 创建工作目录（mkdir -p 语义）。用于终端 spawn 前目录不存在时一键创建 + 重试。
/// 校验路径非空绝对路径后 std::fs::create_dir_all。
#[tauri::command]
#[specta::specta]
pub fn create_directory(path: String) -> Result<(), String> {
    let p = std::path::Path::new(&path);
    if !p.is_absolute() || path.is_empty() {
        return Err(format!("路径无效（须为绝对路径）：{}", path));
    }
    std::fs::create_dir_all(p).map_err(|e| format!("创建目录失败：{}", e))
}

/// 应用退出时（RunEvent::Exit）入口：走 provider 自持的真源 store（spawn
/// 写入侧同一实例）回收全部会话。不能经 State<PtySessionStore>——那是另一
/// 实例恒空（历史 probe 恒 false bug 成因，幽灵 manage 已删，见 state.rs）。
pub fn shutdown_all_provider() {
    shutdown_all(provider().store());
}

/// 遍历 store kill 全部 shell 并清空。
/// reader 线程在 kill 后读到 EOF 自然退出，无需 join。
pub fn shutdown_all(store: &PtySessionStore) {
    let mut map = store
        .0
        .lock()
        .expect("PtySessionStore mutex poisoned");
    let sessions: Vec<_> = map.drain().collect();
    for (_, session) in sessions {
        let session_id = session.session_id.clone();
        if let Err(e) = session.shutdown() {
            log::warn!(
                "[pty] shutdown_all kill {} failed: {}",
                session_id,
                e
            );
        }
    }
    log::info!("[pty] shutdown_all complete");
}
