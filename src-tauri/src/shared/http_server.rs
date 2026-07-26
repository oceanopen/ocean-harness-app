// HTTP 本地服务的进程生命周期管理。
//
// 在 Tauri 应用 setup 阶段（lib.rs）拉起 HTTP 服务：
//   - dev 模式：先 `go build` 同步编译出二进制，再 spawn 该二进制（持有真正的服务进程 handle）
//   - build 模式：spawn 随包分发的二进制 resource_dir/go-server-bin
// dev/build 都 spawn 二进制而非 `go run`：go run 会 fork 临时子进程，Rust 持有的 Child
// 只是 go 工具本身，kill 无法传递给真正的服务进程（孤儿进程）；spawn 二进制则 kill 直接有效。
//
// 退出阶段（RunEvent::Exit）：unix 先 SIGTERM 让服务优雅退出，再 SIGKILL 兜底；
// Windows 直接 terminate（无 SIGTERM 概念）。
//
// 服务固定监听 127.0.0.1:9000（见 src-server/cmd/server/main.go），前端直接 fetch，本模块不涉及端口解析。

use std::io::{BufRead, BufReader};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
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

/// HTTP 服务运行态：持有子进程 handle（用于退出时 kill）、运行模式。
pub struct HttpServerState {
    pub child: Mutex<Option<Child>>,
    pub mode: &'static str,
}

/// 在 setup 阶段拉起 HTTP 服务并注册 HttpServerState 到 app。
/// 失败返回 Err(String)，setup 会因 ? 中断应用启动——HTTP 服务是核心依赖，启动失败应暴露而非静默。
pub fn init(app: &AppHandle) -> Result<(), String> {
    let mode: &'static str = if cfg!(debug_assertions) {
        "dev"
    } else {
        "build"
    };

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

    // stdout 线程：转发日志（服务固定端口 9000，不再解析端口回报）。
    if let Some(stdout) = child.stdout.take() {
        thread::spawn(move || {
            let reader = BufReader::new(stdout);
            for line in reader.lines().flatten() {
                if !line.is_empty() {
                    log::info!("[http-server] stdout: {}", line);
                }
            }
            log::info!("[http-server] stdout stream ended");
        });
    }

    // stderr 线程：转发到 warn 级别日志（服务自身 log 写 stderr）。
    if let Some(stderr) = child.stderr.take() {
        thread::spawn(move || {
            let reader = BufReader::new(stderr);
            for line in reader.lines().flatten() {
                if !line.is_empty() {
                    log::warn!("[http-server] stderr: {}", line);
                }
            }
        });
    }

    app.manage(HttpServerState {
        child: Mutex::new(Some(child)),
        mode,
    });

    log::info!("[http-server] spawned in {mode} mode");
    Ok(())
}

/// 应用退出时（RunEvent::Exit）调用：优雅停止并回收子进程。
/// unix 先发 SIGTERM 让服务优雅退出（关闭监听连接），短暂等待后 SIGKILL 兜底；
/// 用 `kill` 命令发信号避免引入 libc 依赖。Windows 直接 terminate（无 SIGTERM 概念）。
pub fn shutdown(state: &HttpServerState) {
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
