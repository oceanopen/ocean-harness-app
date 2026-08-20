// HTTP 本地服务的进程生命周期管理（sidecar 模式，dev/build 统一）+ IPC 命令。
//
// Go 服务作为 Tauri sidecar 打包，baseName 复用各 conf 的 identifier（让进程名携带环境标识）：
//   build 取 tauri.conf.json、dev 取 tauri.dev.conf.json，文件名 {identifier}-go_server_bin-{triple}。
//   打包器把对应文件去 triple 后缀放进 Contents/MacOS/（macOS）并随主 app 签名——arm64 下 AMFI 不再拦。
//   dev 模式下 tauri-build（cargo build）把该文件拷到 target/<profile>/<baseName>。
//   app.shell().sidecar(format!("{}-go_server_bin", app.config().identifier)) 解析到 current_exe 同级路径，
//   identifier 随当前生效 conf 自动切换，故 dev/build 用同一份代码。
//
// 配置全部走环境变量注入 Go 进程（不读配置文件）：
//   GO_SERVER_MODE（dev 编译→test、build 编译→release）、
//   GO_SERVER_PORT（默认 dev=9000/build=9100，可由系统设置「服务配置」覆盖）、
//   GO_SERVER_LOG_DIR、GO_SERVER_SQLITE_DIR（均由 app_data_dir 派生，dev/build 自动隔离）。
//
// IPC：前端「服务状态」页通过 http_server_status 查询运行态与地址，通过 set_http_server_enabled
//   开关服务（调 start_server/stop_server）。setup 时默认自动启动（开关默认 ON）。
//
// 设计原则：HTTP 服务是旁路（仅 ServerStatusPage 用），不是核心依赖——
//   - init() 永不返回 Err、永不阻塞 setup；目录解析失败仅 log::warn 并跳过自动启动。
//   - app 退出时尽力回收子进程，并通过 aborted 标志消除"退出与注册竞态"产生的孤儿。
//
// 启动阶段（start_server）：停同会话 child + spawn，spawn 后置 Starting；后台 settle 线程在 STARTUP_VERDICT_DEADLINE 窗口内按
//   STARTUP_POLL_INTERVAL 轮询「子进程 PID 是否监听端口」做身份校验——命中即置 Running，窗口结束未命中则 kill 子进程后置 Stopped。
//   运行中进程退出由事件线程的 Terminated 即时捕获。run_state 三态供前端渲染，Running 即自有进程已 bind 可直接 fetch。
//
// 孤立进程清理（cleanup_orphan_http_server 命令）：app 异常退出后跨会话残留的 go-server 可能占用端口，
//   导致新 sidecar bind 失败。前端「服务状态」页开关开启时在 start 前显式调用本命令清理——
//   仅当端口占用者进程名含 tauri.conf identifier + go_server_bin（本应用 sidecar）才 kill，不误杀他应用。
//   init 自动启动场景不清理（按需求仅在前端开关触发）。
//
// 退出阶段（RunEvent::Exit → shutdown）：unix 先 SIGTERM 让服务优雅退出，再 kill() 兜底；
// Windows 直接 kill()（无 SIGTERM 概念）。

use std::fs;
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::sync::Mutex;
use std::sync::atomic::{AtomicBool, AtomicU32, AtomicU8, AtomicU16, Ordering};
use std::thread;
use std::time::{Duration, Instant};

use tauri::{AppHandle, Emitter, Manager, State};
use tauri_plugin_shell::ShellExt;
use tauri_plugin_shell::process::{CommandChild, CommandEvent};

use crate::shared::app_config::{
    AppConfigState, HTTP_SERVER_PORT_KEY, MAX_HTTP_SERVER_PORT, MIN_HTTP_SERVER_PORT,
    read_app_config_raw,
};
use crate::shared::events::EVENT_HTTP_SERVER_STATE_CHANGED;
use crate::shared::types::{HttpServerRunState, HttpServerStatus};

/// dev/build 编译各自的默认端口（用户未在「服务配置」设置 http_server_port 时回退于此）。
/// pub + .constant() 导出到前端 bindings.ts（设置页帮助文案展示用），Rust 单源。
pub const HTTP_SERVER_PORT_TEST: u16 = 9000;
pub const HTTP_SERVER_PORT_RELEASE: u16 = 9100;

/// settle 线程：启动裁定的硬上限，超过仍未监听端口即判定失败（防异常启动永久挂起 Starting）。
const STARTUP_VERDICT_DEADLINE: Duration = Duration::from_secs(10);
/// settle 线程：轮询「子进程是否已监听端口」的间隔。直接探活端口就绪，不依赖日志输出节奏——
/// Go 启动期 SQLite 迁移可能数秒无控制台输出，「日志静默」式触发会误杀正常启动，故改轮询。
const STARTUP_POLL_INTERVAL: Duration = Duration::from_millis(500);

/// 当前编译模式的默认端口（cfg! 编译期决定）。
fn default_port() -> u16 {
    if cfg!(debug_assertions) {
        HTTP_SERVER_PORT_TEST
    } else {
        HTTP_SERVER_PORT_RELEASE
    }
}

/// 解析 HTTP 服务端口：读 app_config 的 http_server_port，合法则用，否则回退模式默认。
/// 在 start_server 调用，保证每次（手动）重启都用最新配置（不监听配置变更）。
fn resolve_server_port(app: &AppHandle) -> u16 {
    let default = default_port();
    let Some(cfg) = app.try_state::<AppConfigState>() else {
        return default;
    };
    let raw = read_app_config_raw(cfg.inner(), HTTP_SERVER_PORT_KEY).unwrap_or(None);
    let Some(raw) = raw.filter(|s| !s.trim().is_empty()) else {
        return default;
    };
    match raw.trim().parse::<u16>() {
        Ok(p) if (MIN_HTTP_SERVER_PORT..=MAX_HTTP_SERVER_PORT).contains(&p) => p,
        _ => {
            log::warn!(
                "[http-server] invalid http_server_port {:?}, fallback to default {}",
                raw,
                default
            );
            default
        }
    }
}

/// HTTP 服务运行态：持有 sidecar 子进程 handle、运行态、运行模式与注入给 Go 的目录。
pub struct HttpServerState {
    /// sidecar 子进程 handle（用于停止时 kill）；None 表示未运行。
    pub child: Mutex<Option<CommandChild>>,
    /// shutdown 置位后，后台 worker 若刚 spawn 出子进程会自行 kill，避免退出竞态产生孤儿。
    pub aborted: AtomicBool,
    /// 运行态（Stopped/Starting/Running），供 http_server_status 查询与前端渲染。
    pub run_state: AtomicU8,
    /// 运行模式（debug/release），对应 Go 的 gin mode。
    pub mode: &'static str,
    /// 监听端口（默认 dev=9000/build=9100；start_server 读 app_config 的 http_server_port 覆盖）。
    pub port: AtomicU16,
    /// 日志目录（注入 GO_SERVER_LOG_DIR）。
    pub log_dir: String,
    /// sqlite 数据目录（注入 GO_SERVER_SQLITE_DIR）。
    pub sqlite_dir: String,
    /// 本次启动过程的全量日志（stdout+stderr，按到达顺序 append）。仅 run_state==Starting 期间累积——
    /// 一旦进入 Running/Stopped 即停止 append，保证前端拿到的是纯净的"本次启动过程"快照，且运行期访问日志不会无限增长。
    /// 失败时取最后一行清洗为 start_last_error；http_server_status 返回前端供后续"查看启动日志"。
    pub start_recent_log: Mutex<Vec<String>>,
    /// 最近一次启动失败的详细原因（启动期进程退出 / 身份校验未通过 / 超时时填充；正常启动 / 主动停止为 None）。
    /// http_server_status 返回给前端，供「服务状态」页重启失败时 toast 展示。
    pub start_last_error: Mutex<Option<String>>,
    /// 本次 sidecar 子进程 PID（spawn 时记录）：settle 线程据「该 PID 是否监听端口」判定启动成败，
    /// 精确匹配端口监听者，区别于占用端口的别家服务或同名 / 孤儿进程。0 表示当前无活动子进程。
    pub child_pid: AtomicU32,
}

impl HttpServerState {
    fn run_state(&self) -> HttpServerRunState {
        match self.run_state.load(Ordering::SeqCst) {
            1 => HttpServerRunState::Starting,
            2 => HttpServerRunState::Running,
            _ => HttpServerRunState::Stopped,
        }
    }
    fn set_run_state(&self, s: HttpServerRunState) {
        self.run_state.store(s as u8, Ordering::SeqCst);
    }
    fn port(&self) -> u16 {
        self.port.load(Ordering::SeqCst)
    }
    fn set_port(&self, port: u16) {
        self.port.store(port, Ordering::SeqCst);
    }
    fn address(&self) -> String {
        format!("http://127.0.0.1:{}", self.port())
    }
    /// 复位启动相关状态（start_server 入口调用）：清启动日志缓冲与 start_last_error，
    /// 确保本次启动过程日志与失败原因取自本次 Go 输出。child_pid 在 spawn 时另行设置。
    fn reset_start_error(&self) {
        if let Ok(mut g) = self.start_recent_log.lock() {
            g.clear();
        }
        if let Ok(mut g) = self.start_last_error.lock() {
            *g = None;
        }
    }
    /// 事件线程 append 一行 stdout/stderr 到启动日志；仅 run_state==Starting 期间累积（一旦 Running/Stopped 即停）。
    fn append_start_recent_log(&self, line: String) {
        if self.run_state() == HttpServerRunState::Starting {
            if let Ok(mut g) = self.start_recent_log.lock() {
                g.push(line);
            }
        }
    }
    /// 设置本次 sidecar 子进程 PID（spawn 时调用）。
    fn set_child_pid(&self, pid: u32) {
        self.child_pid.store(pid, Ordering::SeqCst);
    }
    /// 取启动日志最后一行非空、清洗为失败原因（供 Starting 期进程退出的 error；无内容则 None）。
    fn extract_last_log_error(&self) -> Option<String> {
        self.start_recent_log
            .lock()
            .ok()
            .and_then(|g| {
                g.last()
                    .map(|s| s.trim())
                    .filter(|s| !s.is_empty())
                    .map(str::to_string)
            })
            .map(|line| clean_go_error_line(&line))
            .filter(|s| !s.is_empty())
    }
    /// 清 start_last_error（启动成功 / 主动停止时调用）。
    fn clear_start_last_error(&self) {
        if let Ok(mut g) = self.start_last_error.lock() {
            *g = None;
        }
    }
    /// 取最近一次启动失败原因（http_server_status 返回前端用；无则 None）。
    fn start_last_error(&self) -> Option<String> {
        self.start_last_error
            .lock()
            .ok()
            .and_then(|g| g.clone())
    }
    /// 取启动日志拼接字符串（空则 None），http_server_status 返回前端用。
    fn start_recent_log_joined(&self) -> Option<String> {
        self.start_recent_log
            .lock()
            .ok()
            .filter(|g| !g.is_empty())
            .map(|g| g.join("\n"))
    }
}

/// 清洗 Go 启动失败日志行为干净的错误详情，供前端直接展示（前端只做关键词翻译）。两种来源：
///   - zap console：`2026-07-29 ... FATAL server/main.go:71 serve failed {"error": "..."}` → 提取 error 值
///   - 标准库 log（config.go 的 log.Fatalf）：`2026/07/29 10:52:18 [config] ...` → 剥离前导时间戳
fn clean_go_error_line(line: &str) -> String {
    let line = line.trim();
    // 优先提取 zap 的 error 字段值："error":"..."。
    const NEEDLE: &str = "\"error\":\"";
    if let Some(start) = line.find(NEEDLE) {
        let rest = &line[start + NEEDLE.len()..];
        if let Some(end) = rest.find('"') {
            return rest[..end].trim().to_string();
        }
    }
    // 否则剥离标准库 log 的前导时间戳（格式固定 "YYYY/MM/DD HH:MM:SS "，占 20 字节）。
    let b = line.as_bytes();
    if b.len() > 20
        && b[4] == b'/'
        && b[7] == b'/'
        && b[10] == b' '
        && b[13] == b':'
        && b[16] == b':'
        && b[19] == b' '
    {
        return line[20..].trim().to_string();
    }
    line.to_string()
}

/// 构造当前服务状态快照（run_state/address/port/mode/start_last_error/start_recent_log）。
/// 既是 http_server_status 命令的返回，也作为 state-changed 事件的 payload，保证前端两路数据一致。
fn build_status(state: &HttpServerState) -> HttpServerStatus {
    HttpServerStatus {
        run_state: state.run_state(),
        address: state.address(),
        port: state.port(),
        mode: state.mode.to_string(),
        start_last_error: state.start_last_error(),
        start_recent_log: state.start_recent_log_joined(),
    }
}

/// 更新 run_state 并把最新状态快照作为 payload emit 给前端（前端 listen 后直接同步 UI，无需二次拉取）。
/// 幂等：若新旧状态相同（如多次 Stopped）则不重复 emit——根治事件线程 Terminated+流结束、
/// 以及 settle 线程与 Terminated 并发 finalize 时可能的双重 emit。
/// init 的初始值与 shutdown（app 退出、webview 已关）不走此函数，本就不 emit。
fn transition_state(app: &AppHandle, state: &HttpServerState, new: HttpServerRunState) {
    let old = state.run_state();
    state.set_run_state(new);
    if old == new {
        return;
    }
    let _ = app.emit(
        EVENT_HTTP_SERVER_STATE_CHANGED,
        build_status(state),
    );
}

/// 解析 app_data_dir 下的 app-server 数据目录并确保存在，返回 (log_dir, sqlite_dir)。
/// 命名空间到 app-server/ 子目录，避免与 Rust 自身的 app.db 等文件混放。
fn resolve_dirs(app: &AppHandle) -> Result<(String, String), String> {
    let data_dir: PathBuf = app
        .path()
        .app_data_dir()
        .map_err(|e| format!("resolve app_data_dir failed: {e}"))?;
    let log_dir = data_dir.join("app-server").join("logs");
    let sqlite_dir = data_dir.join("app-server").join("db");
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

// ============================================================
// 端口兜底：清理跨会话残留的 go-server 孤立进程
// ============================================================
// 仅当端口占用者进程名同时含 tauri.conf identifier 与 go_server_bin（即本应用 sidecar）才 kill，
// 避免误杀占用同端口的其它应用（那种情况留给 spawn 的 bind 失败自然暴露）。
// 跨平台：unix 用 lsof/ps/kill，Windows 用 netstat/tasklist/taskkill。

/// 若端口被本应用 go-server 孤立进程占用，逐个 kill（unix SIGTERM→SIGKILL；Windows taskkill /F）。
/// identifier 取自运行态 tauri.conf（app.config().identifier），dev/build 各自匹配自己的 sidecar 名。
fn reclaim_port_if_orphan_go_server(port: u16, identifier: &str) {
    for pid in pids_listening_on_port(port) {
        let cmd = match pid_command(pid) {
            Some(c) => c,
            None => continue,
        };
        if !looks_like_go_server(&cmd, identifier) {
            log::warn!(
                "[http-server] port {} held by non-go-server pid={} ({:?}); not killing",
                port,
                pid,
                cmd
            );
            continue;
        }
        log::warn!(
            "[http-server] killing orphan go-server pid={} on port {}",
            pid,
            port
        );
        kill_orphan_pid(pid);
    }
}

/// 进程命令行是否像本应用 go-server：同时含 **tauri.conf 的 identifier** 与 `go_server_bin`。
/// identifier 由调用方传入运行态 `app.config().identifier`——dev/build 各自取值（见各 conf），
/// 故同一份匹配逻辑天然兼容 dev（...dev-go_server_bin）与 build（...go_server_bin）两种 sidecar 名。
fn looks_like_go_server(cmd: &str, identifier: &str) -> bool {
    cmd.contains(identifier) && cmd.contains("go_server_bin")
}

/// 判断本应用 sidecar 子进程是否已监听目标端口（= Go 已成功 bind）。
/// 复用 pids_listening_on_port 取端口监听者 PID，精确匹配 child_pid——
/// 不受同名 / 孤儿进程干扰，也绝不会把占用端口的别家服务误判为启动成功。
fn child_owns_port(port: u16, child_pid: u32) -> bool {
    pids_listening_on_port(port).contains(&child_pid)
}

#[cfg(unix)]
fn pids_listening_on_port(port: u16) -> Vec<u32> {
    // -nP 不解析主机名/端口名（快且避免 DNS 失败）；-iTCP:<port> 过滤端口；
    // -sTCP:LISTEN 仅监听套接字（bind 冲突的唯一来源）；-t 仅输出 PID。
    let out = Command::new("lsof")
        .args([
            "-nP",
            "-t",
            &format!("-iTCP:{port}"),
            "-sTCP:LISTEN",
        ])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .output();
    match out {
        Ok(o) if o.status.success() => String::from_utf8_lossy(&o.stdout)
            .lines()
            .filter_map(|l| l.trim().parse::<u32>().ok())
            .collect(),
        // lsof 在无匹配时退出码非 0（正常无冲突）；lsof 缺失也走这里，静默跳过。
        _ => Vec::new(),
    }
}

#[cfg(unix)]
fn pid_command(pid: u32) -> Option<String> {
    // command= 输出完整命令行（含可执行文件路径），不被 comm 的 15 字符截断影响（Linux）。
    let out = Command::new("ps")
        .args(["-p", &pid.to_string(), "-o", "command="])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .output()
        .ok()?;
    if !out.status.success() {
        return None;
    }
    let s = String::from_utf8_lossy(&out.stdout)
        .trim()
        .to_owned();
    if s.is_empty() {
        None
    } else {
        Some(s)
    }
}

#[cfg(unix)]
fn pid_alive(pid: u32) -> bool {
    // kill -0 不发信号，仅探测进程是否存在且可信号。
    Command::new("kill")
        .args(["-0", &pid.to_string()])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
}

#[cfg(unix)]
fn kill_orphan_pid(pid: u32) {
    let pid_str = pid.to_string();
    // SIGTERM 优雅退出（关闭监听套接字、释放端口），短暂等待。
    let _ = Command::new("kill")
        .args(["-TERM", pid_str.as_str()])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
    thread::sleep(Duration::from_millis(300));
    // 仍存活则 SIGKILL 兜底。
    if pid_alive(pid) {
        let _ = Command::new("kill")
            .args(["-KILL", pid_str.as_str()])
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status();
    }
}

#[cfg(windows)]
fn pids_listening_on_port(port: u16) -> Vec<u32> {
    // netstat -ano -p TCP 行形如：
    //   TCP    127.0.0.1:9100    0.0.0.0:0    LISTENING    1234
    let out = match Command::new("netstat")
        .args(["-ano", "-p", "TCP"])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .output()
    {
        Ok(o) => o,
        Err(_) => return Vec::new(),
    };
    let suffix = format!(":{port}");
    String::from_utf8_lossy(&out.stdout)
        .lines()
        .filter_map(|line| {
            let f: Vec<&str> = line.split_whitespace().collect();
            // [0]=Proto [1]=本地地址 [2]=外部地址 [3]=状态 [4]=PID
            if f.len() < 5 || f[3] != "LISTENING" || !f[1].ends_with(&suffix) {
                return None;
            }
            f[4].parse::<u32>().ok()
        })
        .collect()
}

#[cfg(windows)]
fn pid_command(pid: u32) -> Option<String> {
    // tasklist CSV 行形如："image.exe","pid","session","sessnum","mem"
    let out = Command::new("tasklist")
        .args([
            "/FI",
            &format!("PID eq {pid}"),
            "/NH",
            "/FO",
            "CSV",
        ])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .output()
        .ok()?;
    // /NH 无表头；PID 不存在时输出 "INFO: No tasks ..." → 不以 " 起首，返回 None。
    let line = String::from_utf8_lossy(&out.stdout)
        .lines()
        .next()?
        .trim()
        .strip_prefix('"')?
        .to_owned();
    let end = line.find('"')?;
    Some(line[..end].to_owned())
}

#[cfg(windows)]
fn kill_orphan_pid(pid: u32) {
    // /F 强制终止；/T 连带子进程（Windows 无 SIGTERM 优雅退出约定）。
    let _ = Command::new("taskkill")
        .args(["/PID", &pid.to_string(), "/F", "/T"])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
}

/// 集中式「置 Stopped + emit + 回收 child」：幂等——已 Stopped 则直接返回不再 emit，
/// 根治事件线程「Terminated + 流结束」双重触发导致的 stopped 双发。
/// error=Some 表示启动失败原因（Starting 期退出 / 身份校验未通过 / 超时），前端据此 toast；
/// error=None 表示运行中或主动停止，不弹失败 toast。
fn finalize_stopped(app: &AppHandle, error: Option<String>) {
    let Some(state) = app.try_state::<HttpServerState>() else {
        return;
    };
    let s = state.inner();
    if s.run_state() == HttpServerRunState::Stopped {
        return;
    }
    if let Ok(mut g) = s.start_last_error.lock() {
        *g = error;
    }
    transition_state(app, s, HttpServerRunState::Stopped);
    if let Ok(mut guard) = s.child.lock() {
        *guard = None;
    }
}

/// 启动 sidecar（仅 spawn + 启动成败判定，不含端口清理逻辑）。在调用方线程同步执行；失败返回 Err。
///
/// 跨会话 go-server 孤立进程的清理已迁移到独立 IPC 命令 `cleanup_orphan_http_server`，
/// 由前端「服务状态」页开关开启时在调用本函数前显式触发（详见该命令注释）。
///
/// 1) 停掉同会话 Rust 持有的 child（强制干净重启；init 时 child 为 None，空操作）。
/// 2) 加锁复查 aborted/dirs（并发双 spawn 由 child 已 Some 兜底）→ spawn → 记 child_pid → 置 Starting；
///    spawn 后再复查 aborted，若期间 app 已退出则当场 kill，不留孤儿。
/// 3) 后台 settle 线程：在 STARTUP_VERDICT_DEADLINE 窗口内按 STARTUP_POLL_INTERVAL 轮询 child_owns_port（PID 精确匹配端口监听者）
///    判定成败——命中即置 Running（前端可直接 fetch），窗口结束未命中则 kill 子进程后置 Stopped(失败)；进程退出由事件线程 Terminated 即时捕获。
fn start_server(app: &AppHandle) -> Result<(), String> {
    let state = app.state::<HttpServerState>();

    // 复位上次的启动错误缓存：start_recent_log 重新累积本次 Go 输出，last_error 等启动裁定结果填充。
    state.reset_start_error();

    // 步骤 1：停同会话 child。单独锁作用域（不与后续 spawn 锁嵌套，避免 std::Mutex 重入死锁）。
    {
        let mut guard = state
            .child
            .lock()
            .map_err(|e| format!("child mutex poisoned: {e}"))?;
        if let Some(child) = guard.take() {
            terminate_child(child);
            transition_state(app, state.inner(), HttpServerRunState::Stopped);
            log::info!("[http-server] stopped existing child before (re)start");
        }
    }

    // 步骤 2：加锁复查 + spawn。
    let mut guard = state
        .child
        .lock()
        .map_err(|e| format!("child mutex poisoned: {e}"))?;

    // 并发兜底：另一个线程可能已在此期间 spawn（本函数不靠 run_state 跳过，改用 child 已 Some 判定）。
    if guard.is_some() {
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

    // 解析端口：读 app_config 的 http_server_port，缺省回退模式默认；更新 state 供 status/settle 使用。
    let port = resolve_server_port(app);
    state.set_port(port);

    // sidecar 名复用当前 identifier（dev/build 各自的 conf 决定），自动区分环境。
    let sidecar_name = format!("{}-go_server_bin", app.config().identifier);
    let (mut rx, child) = app
        .shell()
        .sidecar(&sidecar_name)
        .map_err(|e| format!("resolve {sidecar_name} sidecar failed: {e}"))?
        .env("GO_SERVER_MODE", state.mode)
        .env("GO_SERVER_PORT", port.to_string())
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

    // 记录子进程 PID，供 settle 线程做身份校验（精确匹配端口监听者，区别于别家服务）。
    let child_pid = child.pid();
    state.set_child_pid(child_pid);
    *guard = Some(child);
    transition_state(app, state.inner(), HttpServerRunState::Starting);
    log::info!(
        "[http-server] spawned (mode={}, port={}, pid={}); waiting for startup verdict",
        state.mode,
        port,
        child_pid
    );
    drop(guard); // 释放锁，事件线程与 settle 线程才能访问 child。

    // 后台 settle 线程：判定启动成败。在 STARTUP_VERDICT_DEADLINE 窗口内按 STARTUP_POLL_INTERVAL 轮询 child_owns_port
    // （该 PID 是否监听端口）做身份校验——端口就绪即置 Running；窗口结束仍未就绪则 kill 子进程后置 Stopped(失败)。
    // 轮询而非「日志静默触发」：Go 启动期 SQLite 迁移可能数秒无控制台输出，日志静默会误杀正常启动；
    // 失败即 kill 杜绝「进程在跑但状态 Stopped」的孤儿（settle 一次性判定、运行期无探活，不 kill 则状态永久偏离）。
    // 任一 finalize（进程退出等）先行后，run_state≠Starting 即退让，保证「先到先得」不重复 emit。
    let settle_handle = app.clone();
    thread::spawn(move || {
        let state = settle_handle.state::<HttpServerState>();
        let cap = Instant::now() + STARTUP_VERDICT_DEADLINE;
        let mut ready = false;
        while Instant::now() < cap {
            // 事件线程已 finalize（如启动期进程退出）→ 退让，不重复裁定。
            if state.run_state() != HttpServerRunState::Starting {
                return;
            }
            if child_owns_port(port, child_pid) {
                ready = true;
                break;
            }
            thread::sleep(STARTUP_POLL_INTERVAL);
        }
        // 仍 Starting 才裁定（可能已被事件线程先行 finalize）。
        if state.run_state() != HttpServerRunState::Starting {
            return;
        }
        if ready {
            // 启动成功：清失败原因、置 Running（emit）。日志停止累积（run_state≠Starting）。
            state.clear_start_last_error();
            transition_state(
                &settle_handle,
                state.inner(),
                HttpServerRunState::Running,
            );
            log::info!(
                "[http-server] running (pid {} owns port {})",
                child_pid,
                port
            );
        } else {
            log::warn!(
                "[http-server] startup failed: pid {} not listening on port {} within deadline",
                child_pid,
                port
            );
            // 失败即杀：take 出 child 并 terminate，避免「进程实际在跑却置 Stopped」的孤儿。
            // finalize_stopped 不会再 kill（child 已 take 为 None），仅置状态 + 记录失败原因。
            if let Ok(mut guard) = state.child.lock() {
                if let Some(child) = guard.take() {
                    terminate_child(child);
                }
            }
            finalize_stopped(
                &settle_handle,
                Some("未检测到服务监听端口，可能端口被占用或启动超时".into()),
            );
        }
    });

    // 事件线程：转发 sidecar 的 stdout/stderr/终止/错误。
    // Stderr 用 warn 级：Go 的 zap 控制台日志默认写 stderr（含正常 listening 信息），release 日志级别为 Warn，
    // WARN 才能保证 Go 输出（含失败原因）留痕供排障。
    let app_clone = app.clone();
    thread::spawn(move || {
        while let Some(event) = rx.blocking_recv() {
            match event {
                CommandEvent::Stdout(line) => {
                    let line = String::from_utf8_lossy(&line)
                        .trim_end()
                        .to_owned();
                    if !line.is_empty() {
                        log::info!("[http-server] {}", line);
                        if let Some(state) = app_clone.try_state::<HttpServerState>() {
                            state.append_start_recent_log(line);
                        }
                    }
                }
                CommandEvent::Stderr(line) => {
                    let line = String::from_utf8_lossy(&line)
                        .trim_end()
                        .to_owned();
                    if !line.is_empty() {
                        log::warn!("[http-server] {}", line);
                        // append 到启动日志（zap 改走 stderr 后，启动失败的 Fatal 行也在这里）。
                        if let Some(state) = app_clone.try_state::<HttpServerState>() {
                            state.append_start_recent_log(line);
                        }
                    }
                }
                CommandEvent::Error(err) => log::warn!("[http-server] event error: {}", err),
                CommandEvent::Terminated(_) => {
                    log::info!("[http-server] process terminated");
                    // Starting 期退出 = 启动失败，取末行日志清洗为原因；Running 期退出 = 正常停止，无原因。
                    let error = app_clone
                        .try_state::<HttpServerState>()
                        .filter(|s| s.run_state() == HttpServerRunState::Starting)
                        .and_then(|s| s.extract_last_log_error());
                    finalize_stopped(&app_clone, error);
                }
                _ => {}
            }
        }
        // rx 关闭 = 进程已退出。finalize_stopped 幂等：若 Terminated 已处理则此处 no-op，不再重复 emit。
        finalize_stopped(&app_clone, None);
        log::info!("[http-server] event stream ended");
    });

    Ok(())
}

/// 停止 sidecar（未运行则跳过，幂等）。unix SIGTERM 优雅退出 + kill() 兜底。
fn stop_server(app: &AppHandle) -> Result<(), String> {
    let state = app.state::<HttpServerState>();
    let child = {
        let mut guard = state
            .child
            .lock()
            .map_err(|e| format!("child mutex poisoned: {e}"))?;
        guard.take()
    };
    if let Some(child) = child {
        terminate_child(child);
        // 主动停止非启动失败：finalize_stopped(None) 置 Stopped 且不携带失败原因（不弹失败 toast）；
        // 其后进程退出的 Terminated 会命中 finalize_stopped 幂等分支，不再重复 emit。
        finalize_stopped(app, None);
        log::info!("[http-server] stopped");
    }
    Ok(())
}

/// 在 setup 阶段注册 HttpServerState 并后台拉起 sidecar（默认 ON）。
///
/// 非核心依赖：**永不返回 Err、永不阻塞 setup**。先同步注册 state（含 mode/port/dirs），
/// 确保前端 status 命令立即可用；实际 spawn 在后台线程，失败仅 log::warn。
pub fn init(app: &AppHandle) {
    // app 启动的 go-server 只有两种模式：dev 编译（tauri:dev）→ test，build 编译（tauri:build）→ release。
    let mode = if cfg!(debug_assertions) {
        "test"
    } else {
        "release"
    };

    // 目录解析失败仅告警，state 仍注册（status 命令可用），但跳过自动启动。
    let dirs = match resolve_dirs(app) {
        Ok(d) => Some(d),
        Err(e) => {
            log::warn!(
                "[http-server] resolve dirs failed, auto-start skipped: {}",
                e
            );
            None
        }
    };
    let (log_dir, sqlite_dir) = dirs.clone().unwrap_or_default();

    app.manage(HttpServerState {
        child: Mutex::new(None),
        aborted: AtomicBool::new(false),
        run_state: AtomicU8::new(HttpServerRunState::Stopped as u8),
        mode,
        // 初值取模式默认；实际端口在 start_server 时按 app_config 解析覆盖。
        port: AtomicU16::new(default_port()),
        log_dir,
        sqlite_dir,
        child_pid: AtomicU32::new(0),
        start_recent_log: Mutex::new(Vec::new()),
        start_last_error: Mutex::new(None),
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
            state.set_run_state(HttpServerRunState::Stopped);
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
    build_status(state.inner())
}

/// 开关 HTTP 服务（true=启动，false=停止）。前端 Switch 控件调用。
///
/// 开启时仅调 start_server（内部停同会话 child + spawn，不含端口清理）；
/// 跨会话孤立进程的清理由前端在调用本命令前显式触发 `cleanup_orphan_http_server`。
#[tauri::command]
#[specta::specta]
pub fn set_http_server_enabled(app: AppHandle, enabled: bool) -> Result<(), String> {
    if enabled {
        start_server(&app)
    } else {
        stop_server(&app)
    }
}

/// 清理跨会话残留的本应用 go-server 孤立进程（app 异常退出后残留、占用端口者）。
///
/// 仅当端口占用者进程名同时含 tauri.conf identifier 与 go_server_bin（即本应用 sidecar）才 kill，
/// 不误杀占用同端口的其它应用。前端「服务状态」页开关从关闭→开启时，应在 `set_http_server_enabled(true)`
/// 之前调用本命令，确保端口可用，避免新 sidecar bind 失败。
///
/// 注：init 自动启动场景不调用本命令（按需求仅在服务状态页开关触发）。
#[tauri::command]
#[specta::specta]
pub fn cleanup_orphan_http_server(app: AppHandle) -> Result<(), String> {
    let state = app.state::<HttpServerState>();
    reclaim_port_if_orphan_go_server(state.port(), &app.config().identifier);
    Ok(())
}
