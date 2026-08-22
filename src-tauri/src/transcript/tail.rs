// 增量 follow：轮询订阅的 transcript 文件，读新增字节 → decode → emit transcript:changed
// （terminal_chat T3.2）。
//
// 用轮询而非 notify 单文件监听：transcript 是单文件、append-only，~500ms 轮询已近实时，
// 且绕开文件旋转/截断的 fs 事件边角。对标 sessions 域的 watch/poll 双轨中的 poll 兜底，
// 但此处即时性是主诉求（打字中气泡 + step 级追加）。

use std::io::{Read, Seek, SeekFrom};
use std::time::Duration;

use tauri::{AppHandle, Emitter, Manager};

use crate::shared::events::EVENT_TRANSCRIPT_CHANGED;
use crate::shared::state::transcript::TranscriptWatchStore;
use crate::shared::types::TranscriptChangedPayload;

use super::reader;

/// 轮询间隔：busy turn 中 claude 每 step 落盘，500ms 提供近实时追加。
const TAIL_POLL_MS: u64 = 500;

/// 启动增量 follow 后台线程。线程生命周期与进程一致。
pub fn start(app: AppHandle) {
    std::thread::spawn(move || {
        loop {
            std::thread::sleep(Duration::from_millis(TAIL_POLL_MS));
            // 快照订阅路径（避免持锁遍历期间被 subscribe/unsubscribe 阻塞）。
            let paths: Vec<String> = {
                let store = app.state::<TranscriptWatchStore>();
                let map = store
                    .0
                    .lock()
                    .expect("TranscriptWatchStore mutex poisoned");
                map.keys().cloned().collect()
            };
            for path in paths {
                tail_once(&app, &path);
            }
        }
    });
}

/// 单次追行：读 [offset, size) 新增字节，decode 完整行，推进 offset，emit。
/// 失败静默（文件暂缺/读取失败下轮重试），不阻塞主流程。
fn tail_once(app: &AppHandle, path: &str) {
    let size = match std::fs::metadata(path) {
        Ok(m) => m.len(),
        Err(_) => return, // 文件暂缺（claude 尚未落盘），保持 offset 下轮重试
    };

    let offset = {
        let store = app.state::<TranscriptWatchStore>();
        let map = store
            .0
            .lock()
            .expect("TranscriptWatchStore mutex poisoned");
        // 已退订（start 快照 keys 后前端 unsubscribe）→ 跳过：勿 unwrap_or(0) 当作
        // 新订阅重读重发，也勿重插回 store（否则孤儿条目永久泄漏、每 500ms 空轮询）。
        match map.get(path).copied() {
            Some(o) => o,
            None => return,
        }
    };

    // 文件缩小（session 轮换/截断）→ 重置 offset 重读全量。
    let effective_offset = if size < offset {
        0
    } else {
        offset
    };
    if size == effective_offset {
        return;
    }

    // 读新增字节 [effective_offset, size)。
    let content = match read_range(path, effective_offset, size) {
        Ok(c) => c,
        Err(_) => return,
    };

    // 只 decode 到最后一个 \n：尾部未闭合的 JSON 行是 claude 正在写的一半，等下一轮补齐。
    let complete_len = match content.rfind('\n') {
        Some(i) => i + 1,
        None => return, // 尚无完整行，等下轮
    };
    let messages = reader::read_lines(&content[..complete_len]);
    let new_offset = effective_offset + complete_len as u64;

    {
        let store = app.state::<TranscriptWatchStore>();
        let mut map = store
            .0
            .lock()
            .expect("TranscriptWatchStore mutex poisoned");
        map.insert(path.to_string(), new_offset);
    }

    if !messages.is_empty() {
        if let Err(e) = app.emit(
            EVENT_TRANSCRIPT_CHANGED,
            &TranscriptChangedPayload {
                path: path.to_string(),
                messages,
            },
        ) {
            log::warn!(
                "[transcript] emit transcript:changed failed: {}",
                e
            );
        }
    }
}

/// 读文件 [start, end) 字节范围。seek + read 而非 read_to_string 全量读，
/// 避免大 transcript 每次追行重复读取。偏移恒在行边界（上个完整 \n 之后），
/// 读到的完整行部分 UTF-8 安全；尾部半行即使截断多字节字符也会被丢弃（不 decode）。
fn read_range(path: &str, start: u64, end: u64) -> Result<String, std::io::Error> {
    let mut f = std::fs::File::open(path)?;
    f.seek(SeekFrom::Start(start))?;
    let mut buf = vec![0u8; (end - start) as usize];
    f.read_exact(&mut buf)?;
    Ok(String::from_utf8_lossy(&buf).to_string())
}
