use rusqlite::{Connection, OptionalExtension, params};
use std::sync::Mutex;
use tauri::{AppHandle, Emitter, Manager, State};

use crate::shared::types::AppConfigChangedPayload;

// 界面语言（托盘菜单语言判定读取）。经 .constant() 导出到前端（Rust 单源）。
pub const LANGUAGE_KEY: &str = "language";

/*
 * 桌宠窗口显隐状态。值用 `YesNo` enum（见 types.rs，"Y"/"N"），
 * 缺失视为 `YesNo::Yes`，向后兼容现有用户。
 */
pub const PET_SESSION_SUMMARY_VISIBLE_KEY: &str = "pet_session_summary_visible";

/*
 * sessions 兜底轮询周期（秒）。即时性由 fs watcher 负责，此处仅驱动 Dead 老化与漏报兜底。
 * 本组常量经 build_specta_builder().constant() 导出到前端 bindings.ts（Rust 单源，
 * 前端 appConfig.ts 仅 re-export，不再手工镜像——曾实际漂移：TS 60/5/120 vs Rust 120/10/300）。
 */
pub const POLL_INTERVAL_SECS_KEY: &str = "poll_interval_secs";
pub const DEFAULT_POLL_INTERVAL_SECS: u64 = 120;
pub const MIN_POLL_INTERVAL_SECS: u64 = 10;
pub const MAX_POLL_INTERVAL_SECS: u64 = 300;

/*
 * iTerm2 分屏方向。horizontal = 上下分屏（split horizontally），vertical = 左右分屏（split vertically）。
 * 同经 .constant() 导出（Rust 单源）。
 */
pub const ITERM2_SPLIT_DIRECTION_KEY: &str = "iterm2_split_direction";
pub const DEFAULT_ITERM2_SPLIT_DIRECTION: &str = "horizontal";

/*
 * 打开终端 cd 后追加执行的命令（全局，iTerm2 与 Terminal.app 共用）。
 * 空串 = 仅 cd，不追加。同经 .constant() 导出（Rust 单源）。
 */
pub const TERMINAL_POST_OPEN_COMMAND_KEY: &str = "terminal_post_open_command";
pub const DEFAULT_TERMINAL_POST_OPEN_COMMAND: &str = "";

/*
 * HTTP 本地服务端口（Go sidecar）已彻底固化为编译期常量（dev=9000/release=9100，
 * 见 http_server.rs 端口契约注释），不再提供 http_server_port 配置项。
 * 存量 DB 里的 http_server_port 行无读取者，留置无害、无需迁移。
 */

/*
 * GitHub Personal Access Token（设置 → 个人中心录入，T4.1）。空串 = 未配置。
 * Go sidecar 的 github MCP 工具经 GO_SERVER_APP_DB 指向的本表只读此 key 调 GitHub API。
 * 敏感值：设置页不回显明文（仅示「已配置」），写入即生效（无缓存）。
 */
pub const GITHUB_PAT_KEY: &str = "github_pat";

/*
 * 开机自启动开关：系统登录项状态的本地镜像（系统优先——启动时 shared/autostart.rs 以系统
 * 真实状态回写本 key，保存时经 app-config-changed 事件监听同步系统登录项）。
 * 值用 YesNo（"Y"/"N"），缺失视为 "N"（默认不自启动）。
 */
pub const LAUNCH_AT_LOGIN_KEY: &str = "launch_at_login";

/*
 * ACP 会话闲置回收阈值（分钟）。空闲（就绪态、无进行中回合、无订阅者、无排队回合）超过该时长的 ACP 会话由 Go sidecar 回收（node 进程退出），重开时经锚点 resume 恢复上下文。
 * Go sidecar 经 GO_SERVER_APP_DB 指向的本表只读此 key，到期判定时现读（改配置即时生效）。
 * 值域（5/15/30/60）与脏值回落默认由前端 appConfig.ts 与 Go 侧 parse 对齐约定。
 */
pub const ACP_SESSION_IDLE_TIMEOUT_MINUTES_KEY: &str = "acp_session_idle_timeout_minutes";
pub const DEFAULT_ACP_SESSION_IDLE_TIMEOUT_MINUTES: u64 = 30;

/*
 * bot 忙时新消息处理策略（Go bot 编排器消费）：queue = 排队到队尾（FIFO）；priority =
 * 中断在途回合，新消息插队首，原队列保留继续；interrupt = 中断在途回合并清空排队队列，
 * 仅处理最新消息。Go sidecar 经 GO_SERVER_APP_DB 只读此 key，受理忙时现读（改配置即生效）。
 * 值域与脏值回落默认由前端 appConfig.ts 与 Go 侧 parse 对齐约定。
 */
pub const BOT_INCOMING_POLICY_KEY: &str = "bot_incoming_policy";
pub const DEFAULT_BOT_INCOMING_POLICY: &str = "priority";

pub struct AppConfigState(pub Mutex<Connection>);

pub fn init(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let data_dir = app.path().app_data_dir()?;
    /*
     * dev 构建通过 tauri.dev.conf.json 覆盖 identifier 为 .dev 后缀，
     * 自动隔离到独立 app_data_dir，无需代码层再追加子目录。
     */
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

/*
 * 重置应用设置（数据清理页）：清空 app_config 表全部行，回到「缺失即默认」的出厂态。
 * 在既有 Mutex<Connection> 锁内执行 DELETE，与 get/set 读写天然串行、无并发窗口。
 * 不逐 key 发 app-config-changed 事件：多数 key 的默认值定义在前端 appConfig.ts（Rust 无从得知），「回默认」无法用单 key 事件表达——收敛由调用方随后调用的 restart_app（应用自动重启）统一接管。
 */
#[tauri::command]
#[specta::specta]
pub fn reset_app_config(state: State<'_, AppConfigState>) -> Result<(), String> {
    let conn = state.0.lock().map_err(|e| e.to_string())?;
    conn.execute("DELETE FROM app_config", [])
        .map_err(|e| e.to_string())?;
    Ok(())
}
