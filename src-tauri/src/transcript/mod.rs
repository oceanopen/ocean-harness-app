// transcript 域：解析 `~/.claude/projects/**/*.jsonl` 为结构化消息列表
// （terminal_chat T1.3）。对标 sessions/ 域的单一关注点拆分，本期「最小」范围：
// raw（反序列化单行）→ decode（RawLine → TranscriptMessage）→ reader（全量读）。
// 增量 follow（tail）与事件 store 留 T3.2 流式预览。

pub mod decode;
pub mod raw;
pub mod reader;

use crate::shared::types::TranscriptMessage;

/// 全量读 transcript 文件为消息列表（chat 只读视图数据源）。
/// 前端链路：pty_claude_session 拿 transcript_path → 调本命令 → 渲染。
/// 非法 JSON 行 / 非 user/assistant / 注入 turn → skip 不 panic；
/// 文件不存在 / 读取失败 → Err。
#[tauri::command]
#[specta::specta]
pub fn transcript_read(transcript_path: String) -> Result<Vec<TranscriptMessage>, String> {
    reader::read_file(std::path::Path::new(&transcript_path))
}
