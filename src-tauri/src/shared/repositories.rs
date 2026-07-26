// 本地仓库管理：持久化（复用 app.db 的 AppConfigState 连接）+ git 信息解析 + CRUD 命令。
//
// 设计：
//   - 持久化复用 shared/app_config.rs 的 AppConfigState(Mutex<Connection>)，新增 repositories 表，
//     不另起 State / DB 文件（单连接单库，与 app_config 表共享）。
//   - git 解析复刻 sessions/git.rs 的 `git -C <cwd> ...` 同步阻塞风格（项目不用 tokio），
//     失败字段留空，前端据 card.noRemote / card.noCommit 兜底。
//   - 命令错误统一 Result<T, String>；add 的「非 git 仓库 / 目录已存在」用稳定哨兵字符串
//     （not-a-git-repo / dir-exists），前端字符串匹配后映射 i18n toast key（对齐 navErrToToastKey 思路）。
//   - refresh 不持 Mutex 跑 git：先取 (id, dir) 释放锁，串行解析后再加锁写回，
//     避免 refresh_all 长时间持锁阻塞 app_config 读写（单用户场景 git 串行可接受）。

use std::process::Command;
use std::process::Stdio;

use rusqlite::{Connection, OptionalExtension, params};
use tauri::{Manager, State};

use crate::shared::app_config::AppConfigState;
use crate::shared::types::{RepoSubDir, Repository};

/// 解析得到的 git 信息（内部结构，不跨边界）。
struct RepoInfo {
    remote_url: String,
    branch: String,
    last_commit_at: i64,
    last_commit_message: String,
}

/// init 由 lib.rs setup 在 app_config::init 之后调用（此时 AppConfigState 已 managed）。
/// 复用 AppConfigState 的同一 SQLite 连接建 repositories 表。
pub fn init(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let state = app.state::<AppConfigState>();
    let conn = state.0.lock().map_err(|e| e.to_string())?;
    conn.execute(
        "CREATE TABLE IF NOT EXISTS repositories (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            dir TEXT NOT NULL UNIQUE,
            description TEXT NOT NULL DEFAULT '',
            sub_dir_list TEXT NOT NULL DEFAULT '[]',
            remote_url TEXT NOT NULL DEFAULT '',
            branch TEXT NOT NULL DEFAULT '',
            last_commit_at INTEGER NOT NULL DEFAULT 0,
            last_commit_message TEXT NOT NULL DEFAULT '',
            updated_at INTEGER NOT NULL DEFAULT 0
        )",
        [],
    )?;
    // 兼容老库：CREATE TABLE IF NOT EXISTS 不会给已存在的表补列，这里显式 ALTER 补 description / sub_dir_list。
    ensure_column(
        &conn,
        "repositories",
        "description",
        "TEXT NOT NULL DEFAULT ''",
    )?;
    ensure_column(
        &conn,
        "repositories",
        "sub_dir_list",
        "TEXT NOT NULL DEFAULT '[]'",
    )?;
    Ok(())
}

/// 幂等补列：表已有该列则跳过，否则 `ALTER TABLE ADD COLUMN`。
/// 项目无 schema 版本管理，新增列靠此函数兼容老库——物理列在老库里追加到表尾，但 SELECT 显式列名，
/// 读取顺序由 SELECT_COLS 决定，与物理存储顺序无关（见 map_repo）。
fn ensure_column(
    conn: &Connection,
    table: &str,
    column: &str,
    def: &str,
) -> Result<(), Box<dyn std::error::Error>> {
    let exists = {
        let mut stmt = conn.prepare(&format!("PRAGMA table_info({table})"))?;
        let names: Vec<String> = stmt
            .query_map([], |row| row.get::<_, String>(1))?
            .filter_map(|r| r.ok())
            .collect();
        names.iter().any(|n| n == column)
    };
    if !exists {
        conn.execute(
            &format!("ALTER TABLE {table} ADD COLUMN {column} {def}"),
            [],
        )?;
    }
    Ok(())
}

// ============================================================
// git 解析
// ============================================================

/// 跑 `git -C <dir> <args...>`，成功返回去尾换行的 stdout；失败 / 非 git 目录 → None。
/// stderr null 避免非 git 目录的 `fatal:` 污染父进程终端日志（与 is_dirty 一致）。
fn git_output(dir: &str, args: &[&str]) -> Option<String> {
    let out = Command::new("git")
        .args(["-C", dir])
        .args(args)
        .stderr(Stdio::null())
        .stdout(Stdio::piped())
        .output()
        .ok()?;
    if !out.status.success() {
        return None;
    }
    Some(
        String::from_utf8_lossy(&out.stdout)
            .trim()
            .to_string(),
    )
}

/// 判断 dir 是否为 git 工作区（add 严格校验用）。`--is-inside-work-tree` 成功即视为仓库。
fn is_git_repo(dir: &str) -> bool {
    Command::new("git")
        .args(["-C", dir, "rev-parse", "--is-inside-work-tree"])
        .stderr(Stdio::null())
        .stdout(Stdio::null())
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
}

/// 解析 dir 的 git 信息。任一字段失败留空（remote 无 → ""，detached/无提交 → branch=""，无提交 → 时间 0）。
fn parse_repo_info(dir: &str) -> RepoInfo {
    let remote_url = git_output(dir, &["remote", "get-url", "origin"]).unwrap_or_default();

    // detached HEAD 时 --abbrev-ref 返回 "HEAD"，前端无意义，统一留空。
    // 注意：`--abbrev-ref` 与 `HEAD` 必须是两个独立 argv 元素；若写成 "--abbrev-ref HEAD"
    // 单参数，git rev-parse 会把无法解析为 rev 的非 flag 参数原样回显到 stdout，
    // 导致 branch_raw 取到字符串 "--abbrev-ref HEAD" 而非真实分支名。
    let branch_raw = git_output(dir, &["rev-parse", "--abbrev-ref", "HEAD"]).unwrap_or_default();
    let branch = if branch_raw == "HEAD" {
        String::new()
    } else {
        branch_raw
    };

    // 一次 log 调用同时取提交时间(%ct)与标题(%s)，用换行分隔；subject 不含换行，splitn 安全。
    let (last_commit_at, last_commit_message) =
        match git_output(dir, &["log", "-1", "--format=%ct%n%s"]) {
            Some(s) => {
                let mut parts = s.splitn(2, '\n');
                let ts = parts
                    .next()
                    .and_then(|t| t.trim().parse::<i64>().ok())
                    .unwrap_or(0)
                    * 1000; // 秒 → 毫秒，与 ClaudeSessionInfo 时间戳口径对齐
                let msg = parts.next().unwrap_or("").to_string();
                (ts, msg)
            }
            None => (0, String::new()), // 无提交 / 解析失败
        };

    RepoInfo {
        remote_url,
        branch,
        last_commit_at,
        last_commit_message,
    }
}

/// 归一化项目子目录：去前后空白，剥首尾路径分隔符（`/`/`\`），保证为相对路径。
/// 剥前导分隔符至关重要——`Path::join` 遇到以 `/` 开头的绝对路径会用其整体替换 base，导致拼接出错。
fn normalize_sub_dir(raw: &str) -> String {
    raw.trim()
        .trim_matches(|c| c == '/' || c == '\\')
        .to_string()
}

/// 描述截断到最多 200 个字符（按 Unicode 标量值计），并去首尾空白。
/// 前端已 maxLength 限制，此处防御性兜底绕过 UI 直接调 IPC 的情况（仓库描述与子目录描述共用）。
fn cap_description(raw: &str) -> String {
    let trimmed = raw.trim();
    if trimmed.chars().count() <= 200 {
        return trimmed.to_string();
    }
    trimmed.chars().take(200).collect()
}

/// 校验并归一化用户输入的子目录列表，供 add_repository / update_repository 复用。
/// - 过滤 sub_dir 为空的项（前端已过滤，此处防御）。
/// - 每项 sub_dir 归一化后校验拼接目录存在；任一失败返回哨兵 "invalid-sub-dir"。
/// - 每项 sub_dir_description 截断到 200 字。
/// 调用前须已完成 dir 的 git 仓库校验。
fn normalize_sub_dir_list(dir: &str, raw_list: &[RepoSubDir]) -> Result<Vec<RepoSubDir>, String> {
    let mut out = Vec::with_capacity(raw_list.len());
    for item in raw_list {
        let sub_dir = normalize_sub_dir(&item.sub_dir);
        if sub_dir.is_empty() {
            continue;
        }
        let joined = std::path::Path::new(dir).join(&sub_dir);
        if !joined.is_dir() {
            return Err("invalid-sub-dir".into());
        }
        out.push(RepoSubDir {
            sub_dir,
            sub_dir_description: cap_description(&item.sub_dir_description),
        });
    }
    Ok(out)
}

// ============================================================
// 跨平台打开目录
// ============================================================

/// 用系统默认文件管理器打开目录：macOS Finder(`open`) / Windows Explorer(`explorer`) / Linux(`xdg-open`)。
/// 跨平台 cfg 与 stdio null 写法复刻 panel.rs::open_in_editor。explorer 退出码不可靠，用 spawn 不检查 status。
fn open_dir(dir: &str) -> Result<(), String> {
    #[cfg(target_os = "macos")]
    {
        Command::new("open")
            .arg(dir)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .map_err(|e| format!("failed to open directory: {e}"))?;
    }
    #[cfg(target_os = "windows")]
    {
        Command::new("explorer")
            .arg(dir)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|e| format!("failed to open directory: {e}"))?;
    }
    #[cfg(all(unix, not(target_os = "macos")))]
    {
        Command::new("xdg-open")
            .arg(dir)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .map_err(|e| format!("failed to open directory: {e}"))?;
    }
    Ok(())
}

// ============================================================
// DB CRUD（操作 &Connection，与 app_config.rs::read_app_config_conn 分层一致）
// ============================================================

fn map_repo(row: &rusqlite::Row<'_>) -> rusqlite::Result<Repository> {
    // sub_dir_list 存为 JSON 文本列；解析失败兜底空数组，避免脏数据导致整列表加载失败。
    let sub_dir_list_json: String = row.get(4)?;
    let sub_dir_list =
        serde_json::from_str::<Vec<RepoSubDir>>(&sub_dir_list_json).unwrap_or_default();
    Ok(Repository {
        id: row.get(0)?,
        name: row.get(1)?,
        dir: row.get(2)?,
        description: row.get(3)?,
        sub_dir_list,
        remote_url: row.get(5)?,
        branch: row.get(6)?,
        last_commit_at: row.get(7)?,
        last_commit_message: row.get(8)?,
        updated_at: row.get(9)?,
    })
}
// 注：id 列为 i32（见 types::Repository），rusqlite 自动把 SQLite INTEGER 收窄到 i32（溢出报错，
// 本场景 id 极小不会触发）；last_commit_at/updated_at 为 i64 毫秒时间戳，get 同样直接读取。
// 列顺序由 SELECT_COLS 显式指定，与表物理存储顺序无关（老库 ALTER 追加列在表尾也能正确读取）。

const SELECT_COLS: &str = "id, name, dir, description, sub_dir_list, remote_url, branch, last_commit_at, last_commit_message, updated_at";

fn list_all_conn(conn: &Connection) -> Result<Vec<Repository>, String> {
    // 默认按最近提交时间倒序（无提交 0 沉底），次序按 id 升序稳定。
    let mut stmt = conn
        .prepare(&format!(
            "SELECT {SELECT_COLS} FROM repositories ORDER BY last_commit_at DESC, id ASC"
        ))
        .map_err(|e| e.to_string())?;
    let rows = stmt
        .query_map([], map_repo)
        .map_err(|e| e.to_string())?;
    rows.collect::<Result<Vec<_>, _>>()
        .map_err(|e| e.to_string())
}

fn list_id_dir_conn(conn: &Connection) -> Result<Vec<(i32, String)>, String> {
    let mut stmt = conn
        .prepare("SELECT id, dir FROM repositories")
        .map_err(|e| e.to_string())?;
    let rows = stmt
        .query_map([], |row| {
            Ok((row.get::<_, i32>(0)?, row.get::<_, String>(1)?))
        })
        .map_err(|e| e.to_string())?;
    rows.collect::<Result<Vec<_>, _>>()
        .map_err(|e| e.to_string())
}

fn get_by_id_conn(conn: &Connection, id: i32) -> Result<Repository, String> {
    conn.query_row(
        &format!("SELECT {SELECT_COLS} FROM repositories WHERE id = ?1"),
        params![id],
        map_repo,
    )
    .map_err(|e| e.to_string())
}

fn get_dir_by_id_conn(conn: &Connection, id: i32) -> Result<Option<String>, String> {
    conn.query_row(
        "SELECT dir FROM repositories WHERE id = ?1",
        params![id],
        |row| row.get::<_, String>(0),
    )
    .optional()
    .map_err(|e| e.to_string())
}

fn insert_conn(
    conn: &Connection,
    name: &str,
    dir: &str,
    description: &str,
    sub_dir_list: &[RepoSubDir],
    info: &RepoInfo,
    now: i64,
) -> Result<Repository, String> {
    // sub_dir_list 序列化为 JSON 文本存储；序列化失败兜底空数组，避免阻塞写入。
    let sub_dir_list_json = serde_json::to_string(sub_dir_list).unwrap_or_else(|_| "[]".into());
    conn.execute(
        "INSERT INTO repositories (name, dir, description, sub_dir_list, remote_url, branch, last_commit_at, last_commit_message, updated_at) \
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)",
        params![name, dir, description, sub_dir_list_json, info.remote_url, info.branch, info.last_commit_at, info.last_commit_message, now],
    )
    .map_err(|e| e.to_string())?;
    // last_insert_rowid 返回 i64，收窄为 i32（见 types::Repository 注释，本场景 id 极小）。
    get_by_id_conn(conn, conn.last_insert_rowid() as i32)
}

fn update_info_conn(conn: &Connection, id: i32, info: &RepoInfo, now: i64) -> Result<(), String> {
    conn.execute(
        "UPDATE repositories SET remote_url = ?1, branch = ?2, last_commit_at = ?3, last_commit_message = ?4, updated_at = ?5 WHERE id = ?6",
        params![info.remote_url, info.branch, info.last_commit_at, info.last_commit_message, now, id],
    )
    .map_err(|e| e.to_string())?;
    Ok(())
}

// ============================================================
// 命令
// ============================================================

/// 列出全部仓库（按最近提交时间倒序）。零 git 解析，即时返回。
#[tauri::command]
#[specta::specta]
pub fn list_repositories(state: State<'_, AppConfigState>) -> Result<Vec<Repository>, String> {
    let conn = state.0.lock().map_err(|e| e.to_string())?;
    list_all_conn(&conn)
}

/// 添加仓库。**严格校验**：名称/目录非空、目录为存在的绝对路径、且为 git 仓库；
/// dir 唯一（重复返回哨兵 "dir-exists"）；sub_dir_list 每项拼接目录须存在（否则 "invalid-sub-dir"）。
/// 校验通过后解析 git 信息并入库，返回新仓库。
#[tauri::command]
#[specta::specta]
pub fn add_repository(
    state: State<'_, AppConfigState>,
    name: String,
    dir: String,
    description: String,
    sub_dir_list: Vec<RepoSubDir>,
) -> Result<Repository, String> {
    let name = name.trim();
    let dir = dir.trim();
    // 防御性后端校验：前端 AddRepositoryDialog 的 canSubmit 已禁用空值提交，
    // 此处仅防绕过 UI 直接调 IPC。这两条哨兵前端不映射 i18n（正常路径不可达）。
    if name.is_empty() {
        return Err("name-required".into());
    }
    if dir.is_empty() {
        return Err("dir-required".into());
    }
    let path = std::path::Path::new(&dir);
    if !path.is_absolute() || !path.is_dir() || !is_git_repo(&dir) {
        return Err("not-a-git-repo".into());
    }
    // 子目录列表归一化 + 存在性校验、描述截断。
    let sub_dir_list = normalize_sub_dir_list(&dir, &sub_dir_list)?;
    let description = cap_description(&description);

    // 解析放锁外（git 调用慢），再加锁做唯一性检查 + 插入。
    let info = parse_repo_info(&dir);
    let now = chrono::Utc::now().timestamp_millis();

    let conn = state.0.lock().map_err(|e| e.to_string())?;
    let dup = conn
        .query_row(
            "SELECT 1 FROM repositories WHERE dir = ?1",
            params![dir],
            |_| Ok(()),
        )
        .optional()
        .map_err(|e| e.to_string())?;
    if dup.is_some() {
        return Err("dir-exists".into());
    }
    insert_conn(
        &conn,
        name,
        &dir,
        &description,
        &sub_dir_list,
        &info,
        now,
    )
}

/// 更新仓库的名称、目录、描述与子目录列表。校验新目录须为 git 仓库且不与其他记录重复；
/// sub_dir_list 每项拼接目录须存在。校验通过后重新解析 git 信息并更新，返回更新后的仓库。
#[tauri::command]
#[specta::specta]
pub fn update_repository(
    state: State<'_, AppConfigState>,
    id: i32,
    name: String,
    dir: String,
    description: String,
    sub_dir_list: Vec<RepoSubDir>,
) -> Result<Repository, String> {
    let name = name.trim();
    let dir = dir.trim();
    if name.is_empty() {
        return Err("name-required".into());
    }
    if dir.is_empty() {
        return Err("dir-required".into());
    }
    let path = std::path::Path::new(&dir);
    if !path.is_absolute() || !path.is_dir() || !is_git_repo(&dir) {
        return Err("not-a-git-repo".into());
    }
    let sub_dir_list = normalize_sub_dir_list(&dir, &sub_dir_list)?;
    let description = cap_description(&description);

    let info = parse_repo_info(&dir);
    let now = chrono::Utc::now().timestamp_millis();

    let conn = state.0.lock().map_err(|e| e.to_string())?;
    // dir 唯一性校验：排除自身记录
    let dup = conn
        .query_row(
            "SELECT 1 FROM repositories WHERE dir = ?1 AND id != ?2",
            params![dir, id],
            |_| Ok(()),
        )
        .optional()
        .map_err(|e| e.to_string())?;
    if dup.is_some() {
        return Err("dir-exists".into());
    }

    // sub_dir_list 序列化为 JSON 文本存储。
    let sub_dir_list_json = serde_json::to_string(&sub_dir_list).unwrap_or_else(|_| "[]".into());
    conn.execute(
        "UPDATE repositories SET name = ?1, dir = ?2, description = ?3, sub_dir_list = ?4, remote_url = ?5, branch = ?6, last_commit_at = ?7, last_commit_message = ?8, updated_at = ?9 WHERE id = ?10",
        params![name, dir, description, sub_dir_list_json, info.remote_url, info.branch, info.last_commit_at, info.last_commit_message, now, id],
    )
    .map_err(|e| e.to_string())?;
    get_by_id_conn(&conn, id)
}

/// 删除仓库。
#[tauri::command]
#[specta::specta]
pub fn delete_repository(state: State<'_, AppConfigState>, id: i32) -> Result<(), String> {
    let conn = state.0.lock().map_err(|e| e.to_string())?;
    conn.execute(
        "DELETE FROM repositories WHERE id = ?1",
        params![id],
    )
    .map_err(|e| e.to_string())?;
    Ok(())
}

/// 刷新单个仓库：重解析 git 信息并更新，返回新数据。
#[tauri::command]
#[specta::specta]
pub fn refresh_repository(state: State<'_, AppConfigState>, id: i32) -> Result<Repository, String> {
    // 取 dir（持锁）→ 解析（释放锁跑 git）→ 写回（持锁）→ 返回（持锁）。
    let dir = {
        let conn = state.0.lock().map_err(|e| e.to_string())?;
        get_dir_by_id_conn(&conn, id)?.ok_or_else(|| format!("repository not found: {id}"))?
    };
    let info = parse_repo_info(&dir);
    let now = chrono::Utc::now().timestamp_millis();
    {
        let conn = state.0.lock().map_err(|e| e.to_string())?;
        update_info_conn(&conn, id, &info, now)?;
    }
    let conn = state.0.lock().map_err(|e| e.to_string())?;
    get_by_id_conn(&conn, id)
}

/// 全量刷新：遍历重解析全部仓库并更新，返回新列表。
/// async + spawn_blocking：git 子进程操作让出 async 线程，不阻塞其他 IPC 调用。
#[tauri::command]
#[specta::specta]
pub async fn refresh_all_repositories(
    state: State<'_, AppConfigState>,
) -> Result<Vec<Repository>, String> {
    let entries = {
        let conn = state.0.lock().map_err(|e| e.to_string())?;
        list_id_dir_conn(&conn)?
    };
    let now = chrono::Utc::now().timestamp_millis();
    // 串行解析全部（不持锁）；git 操作通过 spawn_blocking 在阻塞线程池执行，
    // async 线程让出供其他 IPC 使用。
    let infos: Vec<(i32, RepoInfo)> = tauri::async_runtime::spawn_blocking(move || {
        entries
            .iter()
            .map(|(id, dir)| (*id, parse_repo_info(dir)))
            .collect()
    })
    .await
    .map_err(|e| e.to_string())?;
    {
        let conn = state.0.lock().map_err(|e| e.to_string())?;
        // 收集错误而非提前返回：部分仓库 UPDATE 失败时，已成功的更新已落库，
        // 仍返回 DB 最新列表保证 UI 与 DB 一致，失败仅记日志（降级处理）。
        for (id, info) in &infos {
            if let Err(e) = update_info_conn(&conn, *id, info, now) {
                log::warn!(
                    "[repositories] refresh_all update id={} failed: {}",
                    id,
                    e
                );
            }
        }
    }
    let conn = state.0.lock().map_err(|e| e.to_string())?;
    list_all_conn(&conn)
}

/// 用系统文件管理器打开目录。dir 必须为存在的绝对路径。
#[tauri::command]
#[specta::specta]
pub fn open_in_file_manager(dir: String) -> Result<(), String> {
    let path = std::path::Path::new(&dir);
    if dir.is_empty() || !path.is_absolute() {
        return Err("invalid directory".into());
    }
    open_dir(&dir)
}
