// HTTP 本地服务的进程生命周期管理（sidecar 模式，dev/build 统一）。
//
// Go 服务作为 Tauri sidecar（tauri.conf.json bundle.externalBin: "binaries/go-server-bin"）打包：
//   - build 模式：打包器把 binaries/go-server-bin-<triple> 去后缀放进 Contents/MacOS/ 并随主 app 签名
//     ——arm64 下 AMFI 不再拦，正是从 resources 迁到 sidecar 的关键。
//   - dev 模式：tauri-build（cargo build）把该文件拷到 target/<profile>/go-server-bin。
//   app.shell().sidecar("go-server-bin") 解析到 current_exe 同级路径，故 dev/build 用同一份代码。
//
// 设计原则：HTTP 服务是旁路（仅 ServerStatusPage 用），不是核心依赖——
//   - init() 在后台线程异步拉起，永不阻塞 setup、永不返回 Err；
//   - 任意环节失败（二进制缺失 / 签名 / 端口占用 / spawn 报错）仅 log::warn，app 照常运行；
//   - app 退出时尽力回收子进程，并通过 aborted 标志消除"退出与注册竞态"产生的孤儿。
//
// 退出阶段（RunEvent::Exit → shutdown）：unix 先 SIGTERM 让服务优雅退出，再 kill() 兜底；
// Windows 直接 kill()（无 SIGTERM 概念）。
//
// 服务固定监听 127.0.0.1:9000（见 src-server/cmd/server/main.go），前端直接 fetch。

use std::process::{Command, Stdio};
use std::sync::Mutex;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::Duration;

use tauri::{AppHandle, Manager};
use tauri_plugin_shell::ShellExt;
use tauri_plugin_shell::process::{CommandChild, CommandEvent};

/// HTTP 服务运行态：持有 sidecar 子进程 handle（用于退出时 kill）、退出标志、运行模式。
pub struct HttpServerState {
    pub child: Mutex<Option<CommandChild>>,
    /// shutdown 置位后，后台 worker 若刚 spawn 出子进程会自行 kill，避免退出竞态产生孤儿。
    pub aborted: AtomicBool,
    pub mode: &'static str,
}

/// 后台拉起 HTTP 服务 sidecar：spawn 随包（dev 为 target/，build 为 Contents/MacOS/）的 go-server-bin。
/// 仅在后台线程调用；失败返回 Err 由 init 告警，不影响 app 运行。
fn launch(app: &AppHandle, mode: &'static str) -> Result<(), String> {
    // sidecar() 解析到 current_exe 同级的 go-server-bin（dev: target/<profile>/，build: Contents/MacOS/）。
    // 该二进制已由 tauri-build（dev）/ 打包器（build，并签名）放置，arm64 下 AMFI 不再拦。
    let (mut rx, child) = app
        .shell()
        .sidecar("go-server-bin")
        .map_err(|e| format!("resolve go-server-bin sidecar failed ({mode}): {e}"))?
        .env("GO_SERVER_MODE", mode)
        .spawn()
        .map_err(|e| format!("failed to spawn http-server sidecar ({mode}): {e}"))?;

    // 事件线程：转发 sidecar 的 stdout/stderr/终止/错误。
    // Stderr 用 warn 级：Go 的 log 默认写 stderr（含正常 listening 信息），不代表报错；刻意用 WARN
    // 而非 INFO——release 日志级别为 Warn，INFO 会被过滤，WARN 才能保证 Go 输出（含失败原因）留痕供排障。
    thread::spawn(move || {
        while let Some(event) = rx.blocking_recv() {
            match event {
                CommandEvent::Stdout(line) => {
                    let line = String::from_utf8_lossy(&line)
                        .trim_end()
                        .to_owned();
                    if !line.is_empty() {
                        log::info!("[http-server] {}", line);
                    }
                }
                CommandEvent::Stderr(line) => {
                    let line = String::from_utf8_lossy(&line)
                        .trim_end()
                        .to_owned();
                    if !line.is_empty() {
                        log::warn!("[http-server] {}", line);
                    }
                }
                CommandEvent::Error(err) => log::warn!("[http-server] event error: {}", err),
                CommandEvent::Terminated(_) => log::info!("[http-server] process terminated"),
                _ => {}
            }
        }
        log::info!("[http-server] event stream ended");
    });

    // 注册子进程前先抢 child 锁，与 shutdown 互斥，消除"app 退出与注册竞态"产生的孤儿：
    //   - shutdown 尚未置 aborted：本线程存入 child，随后 shutdown 取出 kill；
    //   - shutdown 已置 aborted（app 在 spawn 期间退出）：本线程当场 kill 刚 spawn 的子进程。
    let state = app.state::<HttpServerState>();
    let mut guard = state
        .child
        .lock()
        .map_err(|e| format!("child mutex poisoned: {e}"))?;
    if state.aborted.load(Ordering::SeqCst) {
        drop(guard);
        let _ = child.kill();
        log::warn!("[http-server] app exited during launch; killed spawned child");
        return Ok(());
    }
    *guard = Some(child);
    log::info!("[http-server] spawned in {mode} mode");
    Ok(())
}

/// 在 setup 阶段后台拉起 HTTP 服务 sidecar 并注册 HttpServerState 到 app。
///
/// 非核心依赖：**永不返回 Err、永不阻塞 setup**。先同步注册空 state（child=None），确保
/// RunEvent::Exit 的 try_state 必命中；实际 spawn 在后台线程进行，任意失败仅 log::warn，app 照常运行。
pub fn init(app: &AppHandle) {
    let mode: &'static str = if cfg!(debug_assertions) {
        "dev"
    } else {
        "build"
    };

    app.manage(HttpServerState {
        child: Mutex::new(None),
        aborted: AtomicBool::new(false),
        mode,
    });

    let handle = app.clone();
    thread::spawn(move || {
        if let Err(e) = launch(&handle, mode) {
            log::warn!(
                "[http-server] launch failed (app continues without it; mode={}): {}",
                mode,
                e
            );
        }
    });
}

/// 应用退出时（RunEvent::Exit）调用：优雅停止并回收 sidecar 子进程。
/// 先置 aborted 阻止后台 worker 注册新 spawn 的子进程；再 unix SIGTERM 让服务优雅退出
/// （关闭监听连接），短暂等待后 kill() 兜底；用 `kill` 命令发信号避免引入 libc 依赖。
/// Windows 直接 kill()（无 SIGTERM 概念）。
pub fn shutdown(state: &HttpServerState) {
    // 先置位：后台 worker 若在此之后才 spawn 成功，会读到 aborted 自行 kill，不留孤儿。
    state.aborted.store(true, Ordering::SeqCst);
    if let Ok(mut guard) = state.child.lock() {
        if let Some(child) = guard.take() {
            #[cfg(unix)]
            {
                let pid = child.pid();
                let pid_str = pid.to_string();
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
            log::info!(
                "[http-server] shutdown complete (mode={})",
                state.mode
            );
        }
    }
}
