/*
 * pty 域：应用内 PTY 会话（spawn/写/resize/关闭/reattach）；terminal/ 域负责外部终端跳转，边界互不重叠。
 * 会话锚点统一 `issueId::<paneId>`，cwd 为 `${工作空间目录}/${issueId}`；输出通道 pty_spawn 传 Channel<PtyEvent>（Data/Exit 单通道双分支），spawn 两条路径：裸 shell / direct_command 直启 CLI。
 * 子模块：claude_state 运行态探测 / cli_bin 直启二进制解析 / local_provider 本机实现 / provider trait+入参出参 / session PtySession+SessionIo / state PtySessionStore（抗 webview 刷新常驻）。
 */
pub mod claude_state;
pub mod cli_bin;
pub mod local_provider;
pub mod provider;
pub mod session;
pub mod state;

use local_provider::LocalPtyProvider;
use provider::{PtyProvider, PtyReattached, PtySessionInfo, PtySpawned, SpawnOpts};
use session::PtyEvent;
use state::PtySessionStore;
use tauri::Manager;

/*
 * 全局 provider 实例。Tauri State 管理的是 store，provider 以 once 语义全局唯一
 *（本机后端无状态，仅持 store 引用不便拆双份——直接 lazy 常量）。
 */
fn provider() -> &'static LocalPtyProvider {
    static PROVIDER: std::sync::OnceLock<LocalPtyProvider> = std::sync::OnceLock::new();
    PROVIDER.get_or_init(LocalPtyProvider::new)
}

/*
 * PTY 子进程注入 env：终端基座 + OCEAN_HARNESS_PORT + claude 治理项（禁自更新 /
 * npm 兜底前缀）。无条件覆盖——终端能力以前端 xterm.js 为准，不随启动环境漂移。
 */
fn child_envs(http_port: u16, npm_prefix: Option<&str>) -> Vec<(String, String)> {
    let mut envs = vec![
        (
            "OCEAN_HARNESS_PORT".to_string(),
            http_port.to_string(),
        ),
        ("TERM".to_string(), "xterm-256color".to_string()),
        ("COLORTERM".to_string(), "truecolor".to_string()),
        (
            "TERM_PROGRAM".to_string(),
            "OceanHarness".to_string(),
        ),
        // vendored claude 版本构建期锁死：禁 CLI 自更新，防更新器借 vendored node 跑 npm 写穿打包资源。
        ("DISABLE_AUTOUPDATE".to_string(), "1".to_string()),
    ];
    if let Some(dir) = npm_prefix {
        envs.push(("npm_config_prefix".to_string(), dir.to_string()));
    }
    envs
}

/*
 * 启动/复用会话（幂等）：未退出复用 + 换装 listener；已退出重起（重开语义）。
 * 前端挂载即调本命令；输出/退出事件经 on_event Channel 流式回传。
 * env 注入（仅真正 spawn 新进程时）：终端基座 + OCEAN_HARNESS_PORT（child_envs）——ocean-harness 插件捆绑的 .mcp.json 以 ${OCEAN_HARNESS_PORT:-9100} 展开其 MCP 端点 url，嵌入式终端内的 claude 会话据此连上本进程的 /mcp/streamableHttp/oceanHarness。
 */
#[tauri::command]
#[specta::specta]
pub fn pty_spawn(
    app: tauri::AppHandle,
    opts: SpawnOpts,
    on_event: tauri::ipc::Channel<PtyEvent>,
) -> Result<PtySpawned, String> {
    /*
     * vendored 资源源基准装配期解析一次（幂等；release 走 BaseDirectory::Resource，
     * macOS 打包资源在 .app/Contents/Resources，不可由 exe 路径拼接）。
     */
    cli_bin::ensure_resources_base(&app);
    let http_port = app
        .state::<crate::shared::http_server::HttpServerState>()
        .port
        .load(std::sync::atomic::Ordering::SeqCst);
    // npm 兜底前缀落应用数据目录（随 identifier dev/build 自动隔离）；解析失败仅省略该项。
    let npm_prefix = app.path().app_data_dir().ok().map(|dir| {
        dir.join("npm-prefix")
            .to_string_lossy()
            .into_owned()
    });
    let envs = child_envs(http_port, npm_prefix.as_deref());
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

// 键盘输入写入会话。
#[tauri::command]
#[specta::specta]
pub fn pty_write(session_id: String, data: String) -> Result<(), String> {
    provider().write(&session_id, data.as_bytes())
}

// 终端尺寸变化（xterm onResize）。
#[tauri::command]
#[specta::specta]
pub fn pty_resize(session_id: String, cols: u16, rows: u16) -> Result<(), String> {
    provider().resize(&session_id, cols, rows)
}

// 关闭单个会话（kill shell + 移出 store）。
#[tauri::command]
#[specta::specta]
pub fn pty_shutdown(session_id: String) -> Result<(), String> {
    provider().shutdown(&session_id)
}

/*
 * 关闭整个 issue 的全部 pane 会话（key 以 `issueId::` 为前缀）。
 * issue 删除联动调用（模块 2 split 后一 issue 多 pane，防孤儿会话）。
 */
#[tauri::command]
#[specta::specta]
pub fn pty_shutdown_issue(issue_id: String) -> Result<(), String> {
    provider().shutdown_issue(&issue_id).map(|_| ())
}

// 列出全部会话快照。
#[tauri::command]
#[specta::specta]
pub fn pty_list_sessions() -> Vec<PtySessionInfo> {
    provider().list()
}

// 会话是否存在（含已退出）。前端挂载顺序：exists → 存在则 reattach，不存在才 spawn。
#[tauri::command]
#[specta::specta]
pub fn pty_exists(session_id: String) -> bool {
    provider().exists(&session_id)
}

/*
 * 本会话 shell 子进程树内是否跑着 claude。进程树匹配（claude pid 沿父链找本会话 shell pid），精确到具体终端。
 * 前端事件 + 轮询混合驱动（useClaudeRunning）。
 * 查询必须走 provider() 自持的 store（与 spawn 写入侧同一实例）。
 */
#[tauri::command]
#[specta::specta]
pub fn pty_claude_running(session_id: String) -> bool {
    claude_state::claude_running(provider().store(), &session_id)
}

/*
 * 本会话 shell 子进程树内 claude 进程的 sessionId（终端捕获回写 conversation 锚用）。
 * 无 claude / 会话不在 store 返回 None；查询必须走 provider() 自持的 store。
 */
#[tauri::command]
#[specta::specta]
pub fn pty_claude_session_id(session_id: String) -> Option<String> {
    claude_state::claude_session_id(provider().store(), &session_id)
}

/*
 * 重挂会话（webview 刷新/切换 issue 回切）：ring 快照随返回值送达 + 换装 listener 续流。
 * 已退出会话照常返回（exited=true）；不存在返回 None（前端转 pty_spawn）。
 */
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

/*
 * 创建工作目录（mkdir -p 语义）。用于终端 spawn 前目录不存在时一键创建 + 重试。
 * 校验路径非空绝对路径后 std::fs::create_dir_all。
 */
#[tauri::command]
#[specta::specta]
pub fn create_directory(path: String) -> Result<(), String> {
    let p = std::path::Path::new(&path);
    if !p.is_absolute() || path.is_empty() {
        return Err(format!("路径无效（须为绝对路径）：{}", path));
    }
    std::fs::create_dir_all(p).map_err(|e| format!("创建目录失败：{}", e))
}

/*
 * 应用退出时（RunEvent::Exit）入口：走 provider 自持的真源 store（spawn
 * 写入侧同一实例）回收全部会话。不能经 State<PtySessionStore>——那是另一
 * 实例恒空（历史 probe 恒 false bug 成因，幽灵 manage 已删，见 state.rs）。
 */
pub fn shutdown_all_provider() {
    shutdown_all(provider().store());
}

/*
 * 遍历 store kill 全部 shell 并清空。
 * reader 线程在 kill 后读到 EOF 自然退出，无需 join。
 */
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

#[cfg(test)]
mod tests {
    use super::child_envs;

    // 终端基座 env 齐全且值正确。
    #[test]
    fn child_envs_contains_terminal_base() {
        let envs = child_envs(9000, Some("/tmp/ocean-npm-prefix"));
        let get = |key: &str| {
            envs.iter()
                .find(|(k, _)| k == key)
                .map(|(_, v)| v.as_str())
        };
        assert_eq!(get("OCEAN_HARNESS_PORT"), Some("9000"));
        assert_eq!(get("TERM"), Some("xterm-256color"));
        assert_eq!(get("COLORTERM"), Some("truecolor"));
        assert_eq!(get("TERM_PROGRAM"), Some("OceanHarness"));
        assert_eq!(get("DISABLE_AUTOUPDATE"), Some("1"));
        assert_eq!(
            get("npm_config_prefix"),
            Some("/tmp/ocean-npm-prefix")
        );
    }
}
