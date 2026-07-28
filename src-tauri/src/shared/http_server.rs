// HTTP 本地服务的进程生命周期管理（sidecar 模式，dev/build 统一）+ IPC 命令。
//
// Go 服务作为 Tauri sidecar 打包，baseName 复用各 conf 的 identifier，让进程名携带环境标识：
//   - build：binaries/com.we.claude.terminal-go_server_bin-<triple>（identifier 取自 tauri.conf.json）
//   - dev  ：binaries/com.we.claude.terminal.dev-go_server_bin-<triple>（取自 tauri.dev.conf.json）
//   打包器把对应文件去 triple 后缀放进 Contents/MacOS/（macOS）并随主 app 签名——arm64 下 AMFI 不再拦。
//   dev 模式下 tauri-build（cargo build）把该文件拷到 target/<profile>/<baseName>。
//   app.shell().sidecar(format!("{}-go_server_bin", app.config().identifier)) 解析到 current_exe 同级路径，
//   identifier 随当前生效 conf 自动切换，故 dev/build 用同一份代码。
//
// 配置全部走环境变量注入 Go 进程（不读配置文件）：
//   GO_SERVER_MODE（debug/release）、GO_SERVER_PORT（dev=9000/build=9100，不支持配置化）、
//   GO_SERVER_LOG_DIR、GO_SERVER_SQLITE_DIR（均由 app_data_dir 派生，dev/build 自动隔离）。
//
// IPC：前端「服务状态」页通过 http_server_status 查询运行态与地址，通过 set_http_server_enabled
//   开关服务（调 start_server/stop_server）。setup 时默认自动启动（开关默认 ON）。
//
// 设计原则：HTTP 服务是旁路（仅 ServerStatusPage 用），不是核心依赖——
//   - init() 永不返回 Err、永不阻塞 setup；目录解析失败仅 log::warn 并跳过自动启动。
//   - app 退出时尽力回收子进程，并通过 aborted 标志消除"退出与注册竞态"产生的孤儿。
//
// 退出阶段（RunEvent::Exit → shutdown）：unix 先 SIGTERM 让服务优雅退出，再 kill() 兜底；
// Windows 直接 kill()（无 SIGTERM 概念）。

use std::fs;
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::sync::Mutex;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::Duration;

use tauri::{AppHandle, Manager, State};
use tauri_plugin_shell::ShellExt;
use tauri_plugin_shell::process::{CommandChild, CommandEvent};

use crate::shared::types::HttpServerStatus;

/// dev/release 模式各自的默认端口（不支持配置化，由模式决定，直接注入环境变量）。
const PORT_DEBUG: u16 = 9000;
const PORT_RELEASE: u16 = 9100;

/// HTTP 服务运行态：持有 sidecar 子进程 handle、运行/退出标志、运行模式与注入给 Go 的目录。
pub struct HttpServerState {
    /// sidecar 子进程 handle（用于停止时 kill）；None 表示未运行。
    pub child: Mutex<Option<CommandChild>>,
    /// shutdown 置位后，后台 worker 若刚 spawn 出子进程会自行 kill，避免退出竞态产生孤儿。
    pub aborted: AtomicBool,
    /// 进程是否在运行（供 http_server_status 命令查询，与 child 是否 Some 对应）。
    pub running: AtomicBool,
    /// 运行模式（debug/release），对应 Go 的 gin mode。
    pub mode: &'static str,
    /// 监听端口（dev=9000，build=9100）。
    pub port: u16,
    /// 服务地址（http://127.0.0.1:<port>），前端 fetch 用。
    pub address: String,
    /// 日志目录（注入 GO_SERVER_LOG_DIR）。
    pub log_dir: String,
    /// sqlite 数据目录（注入 GO_SERVER_SQLITE_DIR）。
    pub sqlite_dir: String,
}

/// 解析 app_data_dir 下的 go-server 数据目录并确保存在，返回 (log_dir, sqlite_dir)。
/// 命名空间到 go-server/ 子目录，避免与 Rust 自身的 app.db 等文件混放。
fn resolve_dirs(app: &AppHandle) -> Result<(String, String), String> {
    let data_dir: PathBuf = app
        .path()
        .app_data_dir()
        .map_err(|e| format!("resolve app_data_dir failed: {e}"))?;
    let log_dir = data_dir.join("go-server").join("logs");
    let sqlite_dir = data_dir.join("go-server").join("db");
    fs::create_dir_all(&log_dir).map_err(|e| format!("mkdir log_dir failed: {e}"))?;
    fs::create_dir_all(&sqlite_dir).map_err(|e| format!("mkdir sqlite_dir failed: {e}"))?;
    Ok((
        log_dir.to_string_lossy().into_owned(),
        sqlite_dir.to_string_lossy().into_owned(),
    ))
}

/// 终止子进程：unix 先 SIGTERM 让服务优雅退出（关闭监听连接），短暂等待后 kill() 兜底；
/// Windows 直接 kill()（无 SIGTERM 概念）。用 `kill` 命令发信号避免引入 libc 依赖。
fn terminate_child(child: CommandChild) {
    #[cfg(unix)]
    {
        let pid_str = child.pid().to_string();
        let _ = Command::new("kill")
            .args(["-TERM", pid_str.as_str()])
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status();
        // 给服务优雅退出一丁点时间（本地服务关闭很快），再 kill() 兜底。
        thread::sleep(Duration::from_millis(200));
    }
    let _ = child.kill();
}

/// 进程退出后清运行态 + child（幂等；事件线程与 stop/shutdown 都可能调到）。
fn mark_stopped(app: &AppHandle) {
    if let Some(state) = app.try_state::<HttpServerState>() {
        state.running.store(false, Ordering::SeqCst);
        if let Ok(mut guard) = state.child.lock() {
            *guard = None;
        }
    }
}

/// 启动 sidecar（已在运行则跳过，幂等）。在调用方线程同步执行；失败返回 Err。
///
/// 锁内复查 running 消除并发双启动竞态；spawn 后再复查 aborted，若期间 app 已退出则当场 kill。
fn start_server(app: &AppHandle) -> Result<(), String> {
    let state = app.state::<HttpServerState>();

    let mut guard = state
        .child
        .lock()
        .map_err(|e| format!("child mutex poisoned: {e}"))?;

    // 锁内复查：已运行则跳过；app 已在退出或目录未就绪则拒绝。
    if state.running.load(Ordering::SeqCst) {
        return Ok(());
    }
    if state.aborted.load(Ordering::SeqCst) {
        drop(guard);
        return Err("app is exiting".into());
    }
    if state.log_dir.is_empty() || state.sqlite_dir.is_empty() {
        drop(guard);
        return Err("log_dir/sqlite_dir not resolved".into());
    }

    // sidecar 名复用当前 identifier（dev/build 各自的 conf 决定），自动区分环境：
    //   dev → com.we.claude.terminal.dev-go_server_bin / build → com.we.claude.terminal-go_server_bin
    let sidecar_name = format!("{}-go_server_bin", app.config().identifier);
    let (mut rx, child) = app
        .shell()
        .sidecar(&sidecar_name)
        .map_err(|e| format!("resolve {sidecar_name} sidecar failed: {e}"))?
        .env("GO_SERVER_MODE", state.mode)
        .env("GO_SERVER_PORT", state.port.to_string())
        .env("GO_SERVER_LOG_DIR", &state.log_dir)
        .env("GO_SERVER_SQLITE_DIR", &state.sqlite_dir)
        .spawn()
        .map_err(|e| format!("failed to spawn http-server sidecar: {e}"))?;

    // spawn 期间 app 可能已在退出：当场 kill 刚 spawn 的子进程，不留孤儿。
    if state.aborted.load(Ordering::SeqCst) {
        drop(guard);
        let _ = child.kill();
        log::warn!("[http-server] app exited during start; killed spawned child");
        return Ok(());
    }

    *guard = Some(child);
    state.running.store(true, Ordering::SeqCst);
    log::info!(
        "[http-server] started (mode={}, port={}, addr={})",
        state.mode,
        state.port,
        state.address
    );
    drop(guard); // 释放锁，事件线程才能访问 child。

    // 事件线程：转发 sidecar 的 stdout/stderr/终止/错误。
    // Stderr 用 warn 级：Go 的 zap 控制台日志默认写 stderr（含正常 listening 信息），release 日志级别为 Warn，
    // WARN 才能保证 Go 输出（含失败原因）留痕供排障。
    let app_clone = app.clone();
    thread::spawn(move || {
        while let Some(event) = rx.blocking_recv() {
            match event {
                CommandEvent::Stdout(line) => {
                    let line = String::from_utf8_lossy(&line).trim_end().to_owned();
                    if !line.is_empty() {
                        log::info!("[http-server] {}", line);
                    }
                }
                CommandEvent::Stderr(line) => {
                    let line = String::from_utf8_lossy(&line).trim_end().to_owned();
                    if !line.is_empty() {
                        log::warn!("[http-server] {}", line);
                    }
                }
                CommandEvent::Error(err) => log::warn!("[http-server] event error: {}", err),
                CommandEvent::Terminated(_) => {
                    log::info!("[http-server] process terminated");
                    mark_stopped(&app_clone);
                }
                _ => {}
            }
        }
        // rx 关闭 = 进程已退出，确保运行态清零。
        mark_stopped(&app_clone);
        log::info!("[http-server] event stream ended");
    });

    Ok(())
}

/// 停止 sidecar（未运行则跳过，幂等）。unix SIGTERM 优雅退出 + kill() 兜底。
fn stop_server(app: &AppHandle) -> Result<(), String> {
    let state = app.state::<HttpServerState>();
    let mut guard = state
        .child
        .lock()
        .map_err(|e| format!("child mutex poisoned: {e}"))?;
    if let Some(child) = guard.take() {
        terminate_child(child);
        state.running.store(false, Ordering::SeqCst);
        log::info!("[http-server] stopped");
    }
    Ok(())
}

/// 在 setup 阶段注册 HttpServerState 并后台拉起 sidecar（默认 ON）。
///
/// 非核心依赖：**永不返回 Err、永不阻塞 setup**。先同步注册 state（含 mode/port/dirs），
/// 确保前端 status 命令立即可用；实际 spawn 在后台线程，失败仅 log::warn。
pub fn init(app: &AppHandle) {
    let (mode, port) = if cfg!(debug_assertions) {
        ("debug", PORT_DEBUG)
    } else {
        ("release", PORT_RELEASE)
    };

    // 目录解析失败仅告警，state 仍注册（status 命令可用），但跳过自动启动。
    let dirs = match resolve_dirs(app) {
        Ok(d) => Some(d),
        Err(e) => {
            log::warn!("[http-server] resolve dirs failed, auto-start skipped: {}", e);
            None
        }
    };
    let (log_dir, sqlite_dir) = dirs.clone().unwrap_or_default();

    app.manage(HttpServerState {
        child: Mutex::new(None),
        aborted: AtomicBool::new(false),
        running: AtomicBool::new(false),
        mode,
        port,
        address: format!("http://127.0.0.1:{}", port),
        log_dir,
        sqlite_dir,
    });

    // 默认自动启动（后台线程，不阻塞 setup）。
    if dirs.is_some() {
        let handle = app.clone();
        thread::spawn(move || {
            if let Err(e) = start_server(&handle) {
                log::warn!("[http-server] auto-start failed: {}", e);
            }
        });
    }
}

/// 应用退出时（RunEvent::Exit）调用：置 aborted 阻止新 spawn，再尽力回收 sidecar 子进程。
pub fn shutdown(state: &HttpServerState) {
    // 先置位：后台 worker 若在此之后才 spawn 成功，会读到 aborted 自行 kill，不留孤儿。
    state.aborted.store(true, Ordering::SeqCst);
    if let Ok(mut guard) = state.child.lock() {
        if let Some(child) = guard.take() {
            terminate_child(child);
            state.running.store(false, Ordering::SeqCst);
            log::info!("[http-server] shutdown complete");
        }
    }
}

// ============================================================
// IPC 命令（前端经 tauri-specta 调用，见 src/shared/bindings.ts）
// ============================================================

/// 查询 HTTP 服务运行态与地址。前端 ServerStatusPage 据此渲染 Switch 与服务地址，并 fetch sysinfo。
#[tauri::command]
#[specta::specta]
pub fn http_server_status(state: State<'_, HttpServerState>) -> HttpServerStatus {
    HttpServerStatus {
        running: state.running.load(Ordering::SeqCst),
        address: state.address.clone(),
        port: state.port,
        mode: state.mode.to_string(),
    }
}

/// 开关 HTTP 服务（true=启动，false=停止）。前端 Switch 控件调用。
#[tauri::command]
#[specta::specta]
pub fn set_http_server_enabled(app: AppHandle, enabled: bool) -> Result<(), String> {
    if enabled {
        start_server(&app)
    } else {
        stop_server(&app)
    }
}
