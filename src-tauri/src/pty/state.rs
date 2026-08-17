// PtySessionStore：嵌入式终端会话存储（照 ClaudeSessionStore 范式）。
//
// key 为 issueId（uuid，一 issue 一终端）。webview 刷新 Rust 不死，store 常驻实现抗刷新；
// app 退出时由 lib.rs RunEvent::Exit → pty::shutdown_all 回收（任务 2 接线）。

use std::collections::HashMap;
use std::sync::Mutex;

use tauri::{App, Manager};

use super::session::PtySession;

/// 会话存储。锁 poison 走 expect panic 兜底（与 ClaudeSessionStore 风格一致）：
/// 会话操作均为短临界区（insert/remove/读写句柄），poison 只在持锁线程 panic 时发生，
/// 此时进程状态已不可信，快速失败优于静默死锁。
#[derive(Default)]
pub struct PtySessionStore(pub Mutex<HashMap<String, PtySession>>);

pub fn init(app: &mut App) -> Result<(), Box<dyn std::error::Error>> {
    app.manage(PtySessionStore::default());
    Ok(())
}
