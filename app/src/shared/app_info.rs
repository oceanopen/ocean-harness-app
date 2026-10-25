// 应用元信息查询、数据/日志维护与进程控制命令。

use std::path::Path;
use std::thread;
use std::time::Duration;

use serde::Serialize;
use specta::Type;
use tauri::{AppHandle, Manager};

/*
 * 当前应用名（panel 面包屑根 crumb 等 UI 展示用）。取自 config product_name（与窗口标题同源）：
 * dev 构建经 tauri.dev.conf.json 覆盖为 "Ocean Harness [DEV]"，发布构建为 "Ocean Harness"——前端借此区分当前构建形态（i18n common:brand 无此后缀，仅作异步回填前的同步初值）。缺失时回退品牌名。
 */
#[tauri::command]
#[specta::specta]
pub fn get_app_name(app: AppHandle) -> String {
    app.config()
        .product_name
        .clone()
        .unwrap_or_else(|| "Ocean Harness".to_string())
}

/*
 * 本地数据文件路径（设置 → 数据清理页展示用）。均从 app_data_dir 推导，与
 * app_config::init / http_server::resolve_dirs 同口径——不依赖服务运行态，
 * sidecar 未启动也能拿到路径。
 */
#[derive(Serialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct DataFilePaths {
    // Rust 自身应用设置库（app_config KV 表）文件路径。
    pub app_db_path: String,
    // Go sidecar 业务数据库文件路径（文件名 SSOT 在 Go 侧 sqlite.go，同 README 口径）。
    pub server_db_path: String,
    // Rust 应用日志目录（tauri-plugin-log 写入目标）。
    pub app_log_dir: String,
    // Go sidecar 日志目录（与 http_server 注入 GO_SERVER_LOG_DIR 同口径推导）。
    pub server_log_dir: String,
}

#[tauri::command]
#[specta::specta]
pub fn get_data_file_paths(app: AppHandle) -> Result<DataFilePaths, String> {
    let data_dir = app
        .path()
        .app_data_dir()
        .map_err(|e| format!("resolve app_data_dir failed: {e}"))?;
    let app_log_dir = app
        .path()
        .app_log_dir()
        .map_err(|e| format!("resolve app_log_dir failed: {e}"))?;
    Ok(DataFilePaths {
        app_db_path: data_dir
            .join("app.db")
            .to_string_lossy()
            .into_owned(),
        server_db_path: data_dir
            .join("app-server")
            .join("db")
            .join("server.db")
            .to_string_lossy()
            .into_owned(),
        app_log_dir: app_log_dir.to_string_lossy().into_owned(),
        server_log_dir: data_dir
            .join("app-server")
            .join("logs")
            .to_string_lossy()
            .into_owned(),
    })
}

/*
 * 日志清理（设置 → 数据清理页）：仅删历史/轮转文件，跳过各进程正在写入的当前文件。
 * 命名模式镜像写入方 SSOT：应用侧 tauri-plugin-log 归档 `<productName>_*.log`；
 * 服务侧 Go lumberjack 归档 `app-*.log` / `app.error-*.log`。
 */
#[derive(Serialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct LogCleanupResult {
    // 应用日志目录删除文件数。
    pub app_files: u32,
    // 服务日志目录删除文件数。
    pub server_files: u32,
}

#[tauri::command]
#[specta::specta]
pub fn clean_log_files(app: AppHandle) -> Result<LogCleanupResult, String> {
    let data_dir = app
        .path()
        .app_data_dir()
        .map_err(|e| format!("resolve app_data_dir failed: {e}"))?;
    let app_log_dir = app
        .path()
        .app_log_dir()
        .map_err(|e| format!("resolve app_log_dir failed: {e}"))?;
    let app_prefix = format!("{}_", app.package_info().name);
    let app_files = remove_hist_logs(&app_log_dir, |name| {
        name.starts_with(&app_prefix) && name.ends_with(".log")
    })?;
    let server_files = remove_hist_logs(
        &data_dir.join("app-server").join("logs"),
        |name| {
            (name.starts_with("app-") || name.starts_with("app.error-")) && name.ends_with(".log")
        },
    )?;
    Ok(LogCleanupResult {
        app_files,
        server_files,
    })
}

// 删除目录内命中历史日志命名的文件并返回删除数；目录不存在视为无历史可清。
fn remove_hist_logs(dir: &Path, is_hist: impl Fn(&str) -> bool) -> Result<u32, String> {
    let entries = match std::fs::read_dir(dir) {
        Ok(entries) => entries,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(0),
        Err(e) => return Err(format!("read {} failed: {e}", dir.display())),
    };
    let mut removed = 0;
    for entry in entries {
        let path = entry
            .map_err(|e| format!("read {} failed: {e}", dir.display()))?
            .path();
        if !path.is_file() {
            continue;
        }
        let Some(name) = path.file_name().and_then(|n| n.to_str()) else {
            continue;
        };
        if is_hist(name) {
            std::fs::remove_file(&path)
                .map_err(|e| format!("remove {} failed: {e}", path.display()))?;
            removed += 1;
        }
    }
    Ok(removed)
}

/*
 * 重启应用的唯一入口：托盘菜单、设置页命令等一切重启触点一律经此，禁止直调 `AppHandle::restart()`。
 * 后者在主线程调用走快速路径（直接 spawn 新实例 + exit(0)），跳过 `RunEvent::Exit` 清理，sidecar/PTY 孤儿化占端口、新实例 bind 失败。
 * `request_restart` 线程无关，恒走 request_exit → `RunEvent::Exit`（回收子进程）→ 再重启。
 */
pub fn request_clean_restart(app: &AppHandle) {
    app.request_restart();
}

/*
 * 延迟重启应用（应用设置重置后的收敛手段）：后台线程 sleep ~300ms 让本命令的 IPC
 * 响应先送达前端再重启——避免响应未发出进程就退出、前端悬挂在 pending。
 */
#[tauri::command]
#[specta::specta]
pub fn restart_app(app: AppHandle) {
    thread::spawn(move || {
        thread::sleep(Duration::from_millis(300));
        request_clean_restart(&app);
    });
}
