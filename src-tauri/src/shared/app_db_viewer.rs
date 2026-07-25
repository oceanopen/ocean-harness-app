// 应用库（app.db）原始数据查看器：复用 AppConfigState 连接，提供各表的只读浏览。
//
// 命名：本模块专指「应用内嵌库 app.db」（app_config + repositories 等表）的只读查看，
// 故模块 / 命令 / 跨边界类型一律带 app_db 前缀。未来若新增其他库（如服务端库
// server_db / 缓存库 cache_db），各自独立命名空间，互不冲突。
//
// 设计：
//   - 复用 shared/app_config.rs 的 AppConfigState(Mutex<Connection>)，不另起连接 / 不建表 / 不写数据。
//   - 只读：仅 list_app_db_tables + dump_app_db_table 两个 SELECT 命令，无任何 INSERT/UPDATE/DELETE。
//   - 防注入：SQL 表名无法用 `?N` 占位符参数化，dump_app_db_table 必须先校验表名——
//     白名单标识符字符（`[A-Za-z_][A-Za-z0-9_]*`）+ 须真实存在于 sqlite_master 表清单，
//     双重校验后才拼入 `SELECT * FROM <table>`，杜绝 `; -- '` 等注入载体。
//     （即便 sqlite_master 里存在引号包裹的怪异表名，白名单也会拦截。）
//   - 单元格映射：rusqlite::types::Value → AppDbValue 枚举（serde tag=kind，前端判别渲染）；
//     Blob 不透传二进制（避免控制字符 / 大字节污染前端 JSON），仅传字节数。

use rusqlite::types::Value as SqlValue;
use rusqlite::{Connection, OptionalExtension};
use tauri::State;

use crate::shared::app_config::AppConfigState;
use crate::shared::types::{AppDbTableDump, AppDbTableInfo, AppDbValue};

/// 合法 SQLite 标识符白名单：首字符字母/下划线，其余字母/数字/下划线。
/// 通过此校验的表名可安全拼入 SQL（不含空格 / 引号 / 分号等注入载体）。
fn is_valid_identifier(name: &str) -> bool {
    let mut chars = name.chars();
    match chars.next() {
        Some(c) if c.is_ascii_alphabetic() || c == '_' => {}
        _ => return false,
    }
    chars.all(|c| c.is_ascii_alphanumeric() || c == '_')
}

/// 拉取全部用户表名（排除 sqlite_% 内部表），按名升序。
fn table_names_conn(conn: &Connection) -> Result<Vec<String>, String> {
    let mut stmt = conn
        .prepare(
            "SELECT name FROM sqlite_master \
             WHERE type = 'table' AND name NOT LIKE 'sqlite_%' \
             ORDER BY name ASC",
        )
        .map_err(|e| e.to_string())?;
    let rows = stmt
        .query_map([], |row| row.get::<_, String>(0))
        .map_err(|e| e.to_string())?;
    rows.collect::<Result<Vec<_>, _>>().map_err(|e| e.to_string())
}

/// 把一个 SQLite 动态值映射为跨边界枚举 AppDbValue（serde tag=kind，前端判别渲染）。
fn cell_to_app_db_value(v: SqlValue) -> AppDbValue {
    match v {
        SqlValue::Null => AppDbValue::Null,
        SqlValue::Integer(i) => AppDbValue::Integer { value: i },
        // f64 直接透传（含 NaN/Infinity；前端按需处理，JSON 序列化时 serde 会规整）。
        SqlValue::Real(f) => AppDbValue::Real { value: f },
        SqlValue::Text(s) => AppDbValue::Text { value: s },
        SqlValue::Blob(b) => AppDbValue::Blob { bytes: b.len() as i32 },
    }
}

/// 校验表名并导出整表（列名 + 行）。调用前 `table` 须通过 is_valid_identifier。
fn dump_app_db_table_conn(conn: &Connection, table: &str) -> Result<AppDbTableDump, String> {
    // 白名单通过后表名已无注入载体，但二次校验存在性以防白名单内但不存在 / 已删除的表。
    let exists = conn
        .query_row(
            "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?1",
            rusqlite::params![table],
            |_| Ok(()),
        )
        .optional()
        .map_err(|e| e.to_string())?
        .is_some();
    if !exists {
        return Err(format!("table not found: {table}"));
    }

    let mut stmt = conn
        .prepare(&format!("SELECT * FROM {table}"))
        .map_err(|e| e.to_string())?;
    // 先取列名（owned，结束 stmt 不可变借用）再 query_map（需可变借用）。
    let columns: Vec<String> = stmt
        .column_names()
        .iter()
        .map(|s| (*s).to_string())
        .collect();
    let width = columns.len();
    let rows = stmt
        .query_map([], |row| {
            (0..width)
                .map(|i| {
                    let val: SqlValue = row.get(i)?;
                    Ok::<_, rusqlite::Error>(cell_to_app_db_value(val))
                })
                .collect::<Result<Vec<_>, _>>()
        })
        .map_err(|e| e.to_string())?
        .collect::<Result<Vec<_>, _>>()
        .map_err(|e| e.to_string())?;
    Ok(AppDbTableDump { columns, rows })
}

// ============================================================
// 命令
// ============================================================

/// 列出 app.db 全部用户表及其行数（只读）。前端「应用数据库」页左侧表列表数据源。
#[tauri::command]
#[specta::specta]
pub fn list_app_db_tables(state: State<'_, AppConfigState>) -> Result<Vec<AppDbTableInfo>, String> {
    let conn = state.0.lock().map_err(|e| e.to_string())?;
    let names = table_names_conn(&conn)?;
    let mut out = Vec::with_capacity(names.len());
    for name in names {
        // 表名已来自 sqlite_master；仅常规标识符安全拼接 COUNT，否则记 -1 供前端禁用浏览。
        let row_count = if is_valid_identifier(&name) {
            conn.query_row(
                &format!("SELECT COUNT(*) FROM {name}"),
                [],
                |row| row.get::<_, i64>(0),
            )
            .map(|n| n as i32)
            .unwrap_or(-1)
        } else {
            -1
        };
        out.push(AppDbTableInfo { name, row_count });
    }
    Ok(out)
}

/// 导出 app.db 指定表的列名 + 全部行（只读）。表名经白名单 + 存在性双重校验，防注入。
#[tauri::command]
#[specta::specta]
pub fn dump_app_db_table(
    state: State<'_, AppConfigState>,
    table: String,
) -> Result<AppDbTableDump, String> {
    if !is_valid_identifier(&table) {
        return Err(format!("invalid table name: {table}"));
    }
    let conn = state.0.lock().map_err(|e| e.to_string())?;
    dump_app_db_table_conn(&conn, &table)
}
