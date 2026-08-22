// 全量读 transcript JSONL 文件 → 已解析消息列表（terminal_chat T1.3）。
//
// 逐行 raw::parse + decode，非法 JSON / 非 user/assistant / 注入 turn → skip 不 panic。
// 文件不存在（claude 刚启动尚未产生 transcript）→ Ok(vec![]) 空对话，非错误态；
// 其他读取失败 → Err（chat 视图显示错误态）。

use std::path::Path;

use crate::shared::types::TranscriptMessage;

use super::{decode, raw};

/// 全量读文件为消息列表。文件不存在 → Ok(vec![])（claude 未对话/首个 turn 未落盘，
/// 语义为空对话非错误）；其他 IO 错误 → Err。
pub fn read_file(path: &Path) -> Result<Vec<TranscriptMessage>, String> {
    let content = match std::fs::read_to_string(path) {
        Ok(c) => c,
        // transcript 文件尚未落盘（claude 刚启动未对话）→ 空对话，避免误报错误态。
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(e) => {
            return Err(format!(
                "读取 transcript 失败（{}）：{}",
                path.display(),
                e
            ));
        }
    };
    Ok(read_lines(&content))
}

/// 逐行解析（纯函数，供单测复用，不依赖文件 IO）。
/// 任一非法行 skip 不 panic，保证单行损坏不拖垮整体读取。
pub fn read_lines(content: &str) -> Vec<TranscriptMessage> {
    content
        .lines()
        .filter(|l| !l.trim().is_empty())
        .filter_map(|l| raw::parse(l).ok())
        .filter_map(|r| decode::decode(&r))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    const MULTI_LINE: &str = "\
{\"type\":\"user\",\"uuid\":\"u1\",\"message\":{\"content\":\"hi\"}}
not valid json
{\"type\":\"mode\",\"uuid\":\"m1\",\"message\":{\"content\":\"x\"}}
{\"type\":\"assistant\",\"uuid\":\"a1\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"answer\"}]}}

";

    #[test]
    fn read_lines_skips_invalid_and_unknown() {
        let msgs = read_lines(MULTI_LINE);
        assert_eq!(msgs.len(), 2);
        assert_eq!(msgs[0].id, "u1");
        assert_eq!(msgs[1].id, "a1");
    }

    #[test]
    fn read_lines_empty_returns_empty() {
        assert!(read_lines("").is_empty());
        assert!(read_lines("\n\n  \n").is_empty());
    }

    #[test]
    fn read_file_missing_returns_empty() {
        // 文件不存在（claude 尚未产生 transcript）→ Ok(vec![]) 空对话，非错误态。
        let path = std::path::Path::new("/nonexistent/definitely/missing.jsonl");
        assert!(read_file(path).unwrap().is_empty());
    }
}
