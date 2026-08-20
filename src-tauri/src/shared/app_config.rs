use rusqlite::{Connection, OptionalExtension, params};
use std::sync::Mutex;
use tauri::{AppHandle, Emitter, Manager, State};

use crate::shared::types::AppConfigChangedPayload;

/// 界面语言（托盘菜单语言判定读取）。经 .constant() 导出到前端（Rust 单源）。
pub const LANGUAGE_KEY: &str = "language";

/// 桌宠窗口显隐状态。值用 `YesNo` enum（见 types.rs，"Y"/"N"），
/// 缺失视为 `YesNo::Yes`，向后兼容现有用户。
pub const PET_CLAUDE_SESSIONS_SUMMARY_VISIBLE_KEY: &str = "pet_claude_sessions_summary_visible";

/// 桌宠拖拽开关。值用 `YesNo` enum（"Y"/"N"），缺失视为 `YesNo::No`（默认关闭）：
/// 关闭时点击桌宠打开终端监控页，开启时桌宠可拖拽且点击静默。
pub const PET_CLAUDE_SESSIONS_SUMMARY_DRAGGABLE_KEY: &str = "pet_claude_sessions_summary_draggable";

/// sessions 兜底轮询周期（秒）。即时性由 fs watcher 负责，此处仅驱动 Dead 老化与漏报兜底。
/// 本组常量经 build_specta_builder().constant() 导出到前端 bindings.ts（Rust 单源，
/// 前端 appConfig.ts 仅 re-export，不再手工镜像——曾实际漂移：TS 60/5/120 vs Rust 120/10/300）。
pub const POLL_INTERVAL_SECS_KEY: &str = "poll_interval_secs";
pub const DEFAULT_POLL_INTERVAL_SECS: u64 = 120;
pub const MIN_POLL_INTERVAL_SECS: u64 = 10;
pub const MAX_POLL_INTERVAL_SECS: u64 = 300;

/// iTerm2 分屏方向。horizontal = 上下分屏（split horizontally），vertical = 左右分屏（split vertically）。
/// 同经 .constant() 导出（Rust 单源）。
pub const ITERM2_SPLIT_DIRECTION_KEY: &str = "iterm2_split_direction";
pub const DEFAULT_ITERM2_SPLIT_DIRECTION: &str = "horizontal";

/// 打开终端 cd 后追加执行的命令（全局，iTerm2 与 Terminal.app 共用）。
/// 空串 = 仅 cd，不追加。同经 .constant() 导出（Rust 单源）。
pub const TERMINAL_POST_OPEN_COMMAND_KEY: &str = "terminal_post_open_command";
pub const DEFAULT_TERMINAL_POST_OPEN_COMMAND: &str = "";

/// HTTP 本地服务端口（Go sidecar）。留空或越界=用模式默认（dev 9000 / release 9100，由 http_server 解析）。
/// min/max 对齐 Go sidecar 的端口校验区间。同经 .constant() 导出（Rust 单源）。
pub const HTTP_SERVER_PORT_KEY: &str = "http_server_port";
pub const MIN_HTTP_SERVER_PORT: u16 = 3000;
pub const MAX_HTTP_SERVER_PORT: u16 = 10000;

pub struct AppConfigState(pub Mutex<Connection>);

pub fn init(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let data_dir = app.path().app_data_dir()?;
    // dev 构建通过 tauri.dev.conf.json 覆盖 identifier 为 .dev 后缀，
    // 自动隔离到独立 app_data_dir，无需代码层再追加子目录。
    std::fs::create_dir_all(&data_dir)?;
    let db_path = data_dir.join("app.db");
    let conn = Connection::open(db_path)?;
    conn.execute(
        "CREATE TABLE IF NOT EXISTS app_config (key TEXT PRIMARY KEY, value TEXT NOT NULL)",
        [],
    )?;
    app.manage(AppConfigState(Mutex::new(conn)));
    Ok(())
}

pub fn read_app_config_conn(conn: &Connection, key: &str) -> Result<Option<String>, String> {
    let mut stmt = conn
        .prepare("SELECT value FROM app_config WHERE key = ?1")
        .map_err(|e| e.to_string())?;
    stmt.query_row(params![key], |row| row.get::<_, String>(0))
        .optional()
        .map_err(|e| e.to_string())
}

pub fn read_app_config_raw(state: &AppConfigState, key: &str) -> Result<Option<String>, String> {
    let conn = state.0.lock().map_err(|e| e.to_string())?;
    read_app_config_conn(&conn, key)
}

pub fn write_app_config_conn(conn: &Connection, key: &str, value: &str) -> Result<(), String> {
    conn.execute(
        "INSERT OR REPLACE INTO app_config (key, value) VALUES (?1, ?2)",
        params![key, value],
    )
    .map_err(|e| e.to_string())?;
    Ok(())
}

pub fn write_app_config_raw(state: &AppConfigState, key: &str, value: &str) -> Result<(), String> {
    let conn = state.0.lock().map_err(|e| e.to_string())?;
    write_app_config_conn(&conn, key, value)
}

#[tauri::command]
#[specta::specta]
pub fn get_app_config(
    state: State<'_, AppConfigState>,
    key: String,
) -> Result<Option<String>, String> {
    read_app_config_raw(&state, &key)
}

#[tauri::command]
#[specta::specta]
pub fn set_app_config(
    app: AppHandle,
    state: State<'_, AppConfigState>,
    key: String,
    value: String,
) -> Result<(), String> {
    write_app_config_raw(&state, &key, &value)?;
    app.emit(
        crate::shared::events::EVENT_APP_CONFIG_CHANGED,
        AppConfigChangedPayload { key, value },
    )
    .map_err(|e| e.to_string())?;
    Ok(())
}
