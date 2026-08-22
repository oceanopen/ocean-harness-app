// transcript 域：解析 `~/.claude/projects/**/*.jsonl` 为结构化消息列表
// （terminal_chat T1.3 + T3.2）。对标 sessions/ 域的单一关注点拆分：
//   raw（反序列化单行）→ decode（RawLine → TranscriptMessage）→ reader（全量读）
//   → tail（增量 follow，T3.2 流式预览）。
// 事件 store 为进程内 State（shared::state::transcript），非本域 store.rs。

pub mod decode;
pub mod raw;
pub mod reader;
pub mod tail;

use crate::shared::state::transcript::TranscriptWatchStore;
use crate::shared::types::TranscriptMessage;
use tauri::State;

/// 全量读 transcript 文件为消息列表（chat 只读视图数据源）。
/// 前端链路：pty_claude_session 拿 transcript_path → 调本命令 → 渲染。
/// 非法 JSON 行 / 非 user/assistant / 注入 turn → skip 不 panic；
/// 文件不存在 / 读取失败 → Err。
#[tauri::command]
#[specta::specta]
pub fn transcript_read(transcript_path: String) -> Result<Vec<TranscriptMessage>, String> {
    reader::read_file(std::path::Path::new(&transcript_path))
}

/// 订阅 transcript 增量（terminal_chat T3.2）：全量读初始快照 + 记 offset 开始 watch。
/// 返回初始已解析消息列表；后续新增行经 `transcript:changed` 事件增量推送。
/// 文件暂缺（claude 尚未落盘）→ Ok(vec![]) 空快照，offset=0，watch 就绪等落盘。
#[tauri::command]
#[specta::specta]
pub fn transcript_subscribe(
    state: State<TranscriptWatchStore>,
    transcript_path: String,
) -> Result<Vec<TranscriptMessage>, String> {
    // 读字节 + lossy：半写多字节字符不会像 read_to_string 那样报 InvalidData（容错，
    // 与 tail.read_range 一致）。\n（0x0A）不会出现在多字节序列内部，完整行部分 lossy 恒等。
    let bytes = match std::fs::read(&transcript_path) {
        Ok(b) => b,
        // transcript 尚未落盘（claude 刚启动未对话）→ 空快照，非错误态。
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Vec::new(),
        Err(e) => {
            return Err(format!(
                "读取 transcript 失败（{}）：{}",
                transcript_path, e
            ));
        }
    };
    let content = String::from_utf8_lossy(&bytes).into_owned();
    // offset 记到最后一个完整 `\n` 之后（与 tail_once 半行保护一致）：尾部未闭合的
    // 半行留待 tail 补全后重读，避免把半行字节计入 offset 导致该消息永久丢失。
    let complete_len = content.rfind('\n').map(|i| i + 1).unwrap_or(0);
    let messages = reader::read_lines(&content[..complete_len]);
    let offset = complete_len as u64;
    state
        .0
        .lock()
        .expect("TranscriptWatchStore mutex poisoned")
        .insert(transcript_path, offset);
    Ok(messages)
}

/// 取消订阅：移除 watch 路径（chat 视图卸载 / 切换 issue 时调用）。
#[tauri::command]
#[specta::specta]
pub fn transcript_unsubscribe(state: State<TranscriptWatchStore>, transcript_path: String) {
    state
        .0
        .lock()
        .expect("TranscriptWatchStore mutex poisoned")
        .remove(&transcript_path);
}
