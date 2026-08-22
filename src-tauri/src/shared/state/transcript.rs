use std::collections::HashMap;
use std::sync::Mutex;

use tauri::{App, Manager};

/// transcript 增量 follow 的订阅状态：path → 已读字节偏移（terminal_chat T3.2）。
/// 前端 chat 视图 `transcript_subscribe` 时记入、`transcript_unsubscribe` 移除；
/// tail 后台线程据此追行新增字节。key 为 transcript 文件绝对路径（多 pane 各自不同）。
#[derive(Default)]
pub struct TranscriptWatchStore(pub Mutex<HashMap<String, u64>>);

pub fn init(app: &mut App) -> Result<(), Box<dyn std::error::Error>> {
    app.manage(TranscriptWatchStore::default());
    Ok(())
}
