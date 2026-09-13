use std::collections::HashMap;
use std::sync::Mutex;

use tauri::{App, Manager};

use crate::shared::types::ClaudeSessionInfo;

/// 会话存储。key 为 pid 字符串（与 `~/.claude/sessions/<pid>.json` 文件名一致）。
/// 每次全量 rescan 走替换式写入，保证 Dead 会话立即清除。
#[derive(Default)]
pub struct ClaudeSessionStore(pub Mutex<HashMap<String, ClaudeSessionInfo>>);

/// rescan 互斥锁。store::rescan 有三个并发触发源（watcher / poll / 命令线程），
/// 任一进入后持锁串行执行"读旧快照 → 跑 git → 写回"，避免并发跑 git 串行堆积
/// （多个 idle 会话 × 100ms 量级 git 调用）。poison 走 expect panic 兜底，与
/// write_claude_sessions 风格一致。
#[derive(Default)]
pub struct RescanLock(pub Mutex<()>);

pub fn init(app: &mut App) -> Result<(), Box<dyn std::error::Error>> {
    app.manage(ClaudeSessionStore::default());
    app.manage(RescanLock::default());
    Ok(())
}

/// 替换式写入：clear 后按 pid 为 key 全量 insert。
/// 替换而非合并，保证消失的会话被清除。
pub fn write_claude_sessions(store: &ClaudeSessionStore, sessions: Vec<ClaudeSessionInfo>) {
    let mut map = store
        .0
        .lock()
        .expect("ClaudeSessionStore mutex poisoned");
    map.clear();
    for s in sessions {
        map.insert(s.pid.to_string(), s);
    }
}
