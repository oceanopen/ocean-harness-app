// 应用元信息查询与进程控制命令。

use std::thread;
use std::time::Duration;

use serde::Serialize;
use specta::Type;
use tauri::{AppHandle, Manager};

/// 当前应用名（panel 面包屑根 crumb 等 UI 展示用）。
/// 取自 config product_name（与窗口标题同源）：dev 构建经 tauri.dev.conf.json 覆盖为
/// "Ocean Harness [DEV]"，发布构建为 "Ocean Harness"——前端借此区分当前构建形态
/// （i18n common:brand 无此后缀，仅作异步回填前的同步初值）。缺失时回退品牌名。
#[tauri::command]
#[specta::specta]
pub fn get_app_name(app: AppHandle) -> String {
    app.config()
        .product_name
        .clone()
        .unwrap_or_else(|| "Ocean Harness".to_string())
}

/// 本地数据文件路径（设置 → 数据清理页展示用）。均从 app_data_dir 推导，与
/// app_config::init / http_server::resolve_dirs 同口径——不依赖服务运行态，
/// sidecar 未启动也能拿到路径。
#[derive(Serialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct DataFilePaths {
    /// Rust 自身应用设置库（app_config KV 表）文件路径。
    pub app_db_path: String,
    /// Go sidecar 业务数据库文件路径（文件名 SSOT 在 Go 侧 sqlite.go，同 README 口径）。
    pub server_db_path: String,
}

#[tauri::command]
#[specta::specta]
pub fn get_data_file_paths(app: AppHandle) -> Result<DataFilePaths, String> {
    let data_dir = app
        .path()
        .app_data_dir()
        .map_err(|e| format!("resolve app_data_dir failed: {e}"))?;
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
    })
}

/// 延迟重启应用（应用设置重置后的收敛手段）：后台线程 sleep ~300ms 让本命令的 IPC
/// 响应先送达前端，再 app.restart()——避免响应未发出进程就退出、前端悬挂在 pending。
#[tauri::command]
#[specta::specta]
pub fn restart_app(app: AppHandle) {
    thread::spawn(move || {
        thread::sleep(Duration::from_millis(300));
        app.restart();
    });
}
