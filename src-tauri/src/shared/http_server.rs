// HTTP 本地服务的进程生命周期管理。
//
// 在 Tauri 应用 setup 阶段（lib.rs）后台异步拉起 HTTP 服务：
//   - dev 模式：先 `go build` 同步编译出二进制，再 spawn 该二进制（持有真正的服务进程 handle）
//   - build 模式：spawn 随包分发的二进制 resource_dir/go-server-bin
// dev/build 都 spawn 二进制而非 `go run`：go run 会 fork 临时子进程，Rust 持有的 Child
// 只是 go 工具本身，kill 无法传递给真正的服务进程（孤儿进程）；spawn 二进制则 kill 直接有效。
//
// 设计原则：HTTP 服务是旁路（仅 ServerStatusPage 用），不是核心依赖——
//   - init() 在后台线程异步拉起，永不阻塞 setup、永不返回 Err；
//   - 任意环节失败（二进制缺失 / 签名 / 端口占用 / spawn 报错）仅 log::warn，app 照常运行；
//   - app 退出时尽力回收子进程，并通过 aborted 标志消除"退出与注册竞态"产生的孤儿。
//
// 退出阶段（RunEvent::Exit → shutdown）：unix 先 SIGTERM 让服务优雅退出，再 SIGKILL 兜底；
// Windows 直接 terminate（无 SIGTERM 概念）。
//
// 服务固定监听 127.0.0.1:9000（见 src-server/cmd/server/main.go），前端直接 fetch，本模块不涉及端口解析。

use std::io::{BufRead, BufReader};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::Duration;

use tauri::{AppHandle, Manager};

/// HTTP 服务二进制文件名（Windows 带 .exe 后缀）。产物名保留 go-server-bin（Go 编译产物）。
fn bin_name() -> &'static str {
    if cfg!(target_os = "windows") {
        "go-server-bin.exe"
    } else {
        "go-server-bin"
    }
}

/// macOS GUI 应用从 .app bundle 启动时继承的 PATH 不含 /opt/homebrew/bin、/usr/local/bin，
/// 直接 spawn `go` 会 ENOENT。dev 模式需 go build 编译，故注入常见安装路径到 PATH 前部。
#[cfg(target_os = "macos")]
fn enrich_path(cmd: &mut Command) {
    const EXTRA: &[&str] = &[
        "/opt/homebrew/bin",
        "/usr/local/bin",
        "/usr/bin",
        "/bin",
    ];
    let existing = std::env::var("PATH").unwrap_or_default();
    let merged: Vec<String> = EXTRA
        .iter()
        .map(|s| (*s).to_string())
        .chain(std::iter::once(existing))
        .collect();
    cmd.env("PATH", merged.join(":"));
}

/// HTTP 服务运行态：持有子进程 handle（用于退出时 kill）、退出标志、运行模式。
pub struct HttpServerState {
    pub child: Mutex<Option<Child>>,
    /// shutdown 置位后，后台 worker 若刚 spawn 出子进程会自行 kill，避免退出竞态产生孤儿。
    pub aborted: AtomicBool,
    pub mode: &'static str,
}

/// 后台拉起 HTTP 服务：dev 先 go build 再 spawn 二进制，build spawn 随包二进制。
/// 仅在后台线程调用；失败返回 Err 由 init 告警，不影响 app 运行。
fn launch(app: &AppHandle, mode: &'static str) -> Result<(), String> {
    let mut cmd = if mode == "dev" {
        // dev：先同步 go build 出二进制，再 spawn 该二进制。
        // 不用 go run：go run 会 fork 临时子进程跑编译产物，Rust 持有的 Child 只是 go 工具本身，
        // kill 无法传递给真正的服务进程（孤儿进程）。spawn 二进制则 kill 直接有效。
        let src_go = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join("..")
            .join("src-server");
        // dev 二进制输出到系统 temp 目录：写入 src-tauri/resources/ 会触发 Tauri dev 文件
        // watcher（监听 src-tauri/）"Rebuilding application" 死循环。temp 脱离项目目录，watcher
        // 不监听；每次 dev 启动覆盖同一文件。build 模式才用 src-tauri/resources/（需打包）。
        let dev_bin = std::env::temp_dir().join(format!("we-claude-terminal-{}", bin_name()));

        let mut build = Command::new("go");
        build
            .arg("build")
            .arg("-o")
            .arg(&dev_bin)
            .arg("./cmd/server")
            .current_dir(&src_go);
        #[cfg(target_os = "macos")]
        enrich_path(&mut build);

        let status = build
            .status()
            .map_err(|e| format!("failed to run go build ({mode}): {e}"))?;
        if !status.success() {
            return Err(format!(
                "go build failed ({mode}); see stderr output"
            ));
        }

        Command::new(dev_bin)
    } else {
        // build：随包分发的二进制（tauri.conf.json bundle.resources）。
        let resource_dir = app
            .path()
            .resource_dir()
            .map_err(|e| format!("resolve resource_dir failed: {e}"))?;
        Command::new(resource_dir.join(bin_name()))
    };

    cmd.env("GO_SERVER_MODE", mode)
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());

    let mut child = cmd
        .spawn()
        .map_err(|e| format!("failed to spawn http-server ({mode}): {e}"))?;

    // stdout 转发（info 级）。Go 服务目前只用 log（写 stderr），stdout 实际无输出，保留兜底。
    if let Some(stdout) = child.stdout.take() {
        thread::spawn(move || {
            let reader = BufReader::new(stdout);
            for line in reader.lines().flatten() {
                if !line.is_empty() {
                    log::info!("[http-server] {}", line);
                }
            }
            log::info!("[http-server] stdout stream ended");
        });
    }

    // stderr 转发（warn 级）。Go 的 log 标准库默认写 stderr——含正常的 listening 启动信息，
    // 不代表报错；故用中性 [http-server] 前缀（不写 "stderr"，避免正常输出看起来像错误）。
    // 刻意用 WARN 而非 INFO：release 日志级别为 Warn，INFO 会被过滤；WARN 才能保证 Go 的输出
    // （含失败原因）在 release 日志留痕供排障。
    if let Some(stderr) = child.stderr.take() {
        thread::spawn(move || {
            let reader = BufReader::new(stderr);
            for line in reader.lines().flatten() {
                if !line.is_empty() {
                    log::warn!("[http-server] {}", line);
                }
            }
        });
    }

    // 注册子进程前先抢 child 锁，与 shutdown 互斥，消除"app 退出与注册竞态"产生的孤儿：
    //   - shutdown 尚未置 aborted：本线程存入 child，随后 shutdown 取出 kill；
    //   - shutdown 已置 aborted（app 在 go build/spawn 期间退出）：本线程当场 kill 刚 spawn 的子进程。
    let state = app.state::<HttpServerState>();
    let mut guard = state
        .child
        .lock()
        .map_err(|e| format!("child mutex poisoned: {e}"))?;
    if state.aborted.load(Ordering::SeqCst) {
        drop(guard);
        let _ = child.kill();
        let _ = child.wait();
        log::warn!("[http-server] app exited during launch; killed spawned child");
        return Ok(());
    }
    *guard = Some(child);
    log::info!("[http-server] spawned in {mode} mode");
    Ok(())
}

/// 在 setup 阶段后台拉起 HTTP 服务并注册 HttpServerState 到 app。
///
/// 非核心依赖：**永不返回 Err、永不阻塞 setup**。先同步注册空 state（child=None），确保
/// RunEvent::Exit 的 try_state 必命中；实际 go build + spawn 在后台线程进行，任意失败仅 log::warn，
/// app 照常运行。
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

/// 应用退出时（RunEvent::Exit）调用：优雅停止并回收子进程。
/// 先置 aborted 阻止后台 worker 注册新 spawn 的子进程；再 unix SIGTERM 让服务优雅退出
/// （关闭监听连接），短暂等待后 SIGKILL 兜底；用 `kill` 命令发信号避免引入 libc 依赖。
/// Windows 直接 terminate（无 SIGTERM 概念）。
pub fn shutdown(state: &HttpServerState) {
    // 先置位：后台 worker 若在此之后才 spawn 成功，会读到 aborted 自行 kill，不留孤儿。
    state.aborted.store(true, Ordering::SeqCst);
    if let Ok(mut guard) = state.child.lock() {
        if let Some(mut child) = guard.take() {
            #[cfg(unix)]
            {
                let pid = child.id();
                let pid_str = pid.to_string();
                let _ = Command::new("kill")
                    .args(["-TERM", pid_str.as_str()])
                    .stdin(Stdio::null())
                    .stdout(Stdio::null())
                    .stderr(Stdio::null())
                    .status();
                // 给服务优雅退出一丁点时间（本地服务关闭很快），再 SIGKILL 兜底。
                thread::sleep(Duration::from_millis(200));
            }
            let _ = child.kill();
            let _ = child.wait();
            log::info!(
                "[http-server] shutdown complete (mode={})",
                state.mode
            );
        }
    }
}
