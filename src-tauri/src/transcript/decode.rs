// RawLine → TranscriptMessage：type 过滤 + 注入 turn 过滤 + content block 映射 +
// role 推导 + ISO8601 时间戳解析（terminal_chat T1.3）。
//
// 容错（KTD）：未知 type / 未知 block / 非法时间戳 / 缺 uuid → None（skip 不 panic）。
// 注入 turn（isMeta/isSynthetic/isCompactSummary）仅保留 tool_result block，其余丢弃
// （对齐 orca：Claude 把系统提示注入成 user 行，不渲染成用户气泡）。

use crate::shared::types::{TranscriptBlock, TranscriptMessage, TranscriptRole};

use super::raw::{RawBlock, RawContent, RawLine};

/// 单行 RawLine → Option<TranscriptMessage>。
/// 非 user/assistant、注入 turn 无 tool_result、无有效 block、缺 uuid → None。
pub fn decode(line: &RawLine) -> Option<TranscriptMessage> {
    let is_user = line.type_ == "user";
    let is_assistant = line.type_ == "assistant";
    if !is_user && !is_assistant {
        return None;
    }

    // 提取原始 blocks：content 为 string 时转单 text block；array 时逐块映射。
    let raw_blocks = match line.message.as_ref()?.content.as_ref()? {
        RawContent::String(s) => {
            let text = s.trim();
            if text.is_empty() {
                Vec::new()
            } else {
                vec![TranscriptBlock::Text { text: s.clone() }]
            }
        }
        RawContent::Array(arr) => arr
            .iter()
            .filter_map(block_to_transcript)
            .collect(),
    };

    // 注入 user turn：仅保留 tool_result（系统提示注入不渲染成用户气泡）。
    let injected = is_user
        && (line.is_meta == Some(true)
            || line.is_synthetic == Some(true)
            || line.is_compact_summary == Some(true));
    let blocks: Vec<TranscriptBlock> = if injected {
        raw_blocks
            .into_iter()
            .filter(|b| matches!(b, TranscriptBlock::ToolResult { .. }))
            .collect()
    } else {
        raw_blocks
    };
    if blocks.is_empty() {
        return None;
    }

    let role = message_role(is_user, &blocks);
    let timestamp = line
        .timestamp
        .as_deref()
        .and_then(parse_timestamp_ms);
    Some(TranscriptMessage {
        id: line.uuid.clone()?,
        role,
        blocks,
        timestamp,
    })
}

/// user 行仅含 tool_result 时视为 tool 角色（工具执行结果回流，非用户真话）。
fn message_role(is_user: bool, blocks: &[TranscriptBlock]) -> TranscriptRole {
    if is_user {
        let only_tool_results = !blocks.is_empty()
            && blocks
                .iter()
                .all(|b| matches!(b, TranscriptBlock::ToolResult { .. }));
        if only_tool_results {
            TranscriptRole::Tool
        } else {
            TranscriptRole::User
        }
    } else {
        TranscriptRole::Assistant
    }
}

/// 单个 content block → TranscriptBlock。未知 type / 空正文 → None。
fn block_to_transcript(b: &RawBlock) -> Option<TranscriptBlock> {
    match b.type_.as_str() {
        "text" => {
            let text = b.text.as_deref()?.trim();
            if text.is_empty() {
                None
            } else {
                Some(TranscriptBlock::Text {
                    text: text.to_string(),
                })
            }
        }
        "thinking" => {
            // thinking 正文优先取 thinking 字段，回落 text。
            let text = b
                .thinking
                .as_deref()
                .or(b.text.as_deref())?
                .trim();
            if text.is_empty() {
                None
            } else {
                Some(TranscriptBlock::Thinking {
                    text: text.to_string(),
                })
            }
        }
        "tool_use" => {
            let name = b.name.clone()?;
            let input = b.input.as_ref().map(|v| v.to_string());
            Some(TranscriptBlock::ToolCall { name, input })
        }
        "tool_result" => {
            let output = tool_result_output(b.content.as_ref());
            Some(TranscriptBlock::ToolResult {
                output,
                is_error: b.is_error == Some(true),
            })
        }
        "image" => {
            // 仅处理带 url/path 的图片引用；base64 内联图无 url/path，丢弃（对齐 orca）。
            let url = b.url.clone();
            let path = b.path.clone();
            if url.is_none() && path.is_none() {
                return None;
            }
            Some(TranscriptBlock::Image {
                url,
                path,
                alt: b.alt.clone(),
            })
        }
        _ => None,
    }
}

/// 把任意 tool_result content 归一化为字符串（对齐 orca toolResultOutput）。
/// string 直用；array 逐项取 text/content；对象取 text/content；其余 JSON 序列化。
fn tool_result_output(value: Option<&serde_json::Value>) -> String {
    let Some(v) = value else {
        return String::new();
    };
    match v {
        serde_json::Value::String(s) => s.clone(),
        serde_json::Value::Array(items) => {
            let mut parts = Vec::new();
            for item in items {
                match item {
                    serde_json::Value::String(s) => parts.push(s.clone()),
                    other => {
                        if let Some(serde_json::Value::String(s)) =
                            other.get("text").or_else(|| other.get("content"))
                        {
                            parts.push(s.clone());
                        }
                    }
                }
            }
            parts.join("\n")
        }
        serde_json::Value::Null => String::new(),
        other => {
            if let Some(serde_json::Value::String(s)) =
                other.get("text").or_else(|| other.get("content"))
            {
                s.clone()
            } else {
                other.to_string()
            }
        }
    }
}

/// ISO8601（RFC3339）→ 毫秒时间戳。非法/缺失返回 None（容错）。
fn parse_timestamp_ms(s: &str) -> Option<i64> {
    chrono::DateTime::parse_from_rfc3339(s)
        .ok()
        .map(|dt| dt.timestamp_millis())
}

#[cfg(test)]
mod tests {
    use super::super::raw;
    use super::*;

    fn decode_str(line: &str) -> Option<TranscriptMessage> {
        decode(&raw::parse(line).unwrap())
    }

    #[test]
    fn decodes_user_string() {
        let m = decode_str(
            r#"{"type":"user","uuid":"u1","timestamp":"2026-08-22T09:21:18.108Z","message":{"content":"hi"}}"#,
        )
        .unwrap();
        assert_eq!(m.id, "u1");
        assert_eq!(m.role, TranscriptRole::User);
        assert_eq!(m.blocks.len(), 1);
        assert!(matches!(
            m.blocks[0],
            TranscriptBlock::Text { .. }
        ));
        assert!(m.timestamp.is_some());
    }

    #[test]
    fn decodes_assistant_blocks() {
        let m = decode_str(
            r#"{"type":"assistant","uuid":"a1","message":{"content":[{"type":"thinking","thinking":"reason"},{"type":"text","text":"answer"},{"type":"tool_use","name":"Read","input":{"path":"a.ts"}}]}}"#,
        )
        .unwrap();
        assert_eq!(m.role, TranscriptRole::Assistant);
        assert_eq!(m.blocks.len(), 3);
        assert!(matches!(
            m.blocks[0],
            TranscriptBlock::Thinking { .. }
        ));
        assert!(matches!(
            m.blocks[1],
            TranscriptBlock::Text { .. }
        ));
        assert!(matches!(
            m.blocks[2],
            TranscriptBlock::ToolCall { .. }
        ));
    }

    #[test]
    fn filters_injected_user_turn_without_tool_result() {
        // isMeta user 仅 text（系统提示注入）→ 完全丢弃
        let m = decode_str(
            r#"{"type":"user","uuid":"u1","isMeta":true,"message":{"content":[{"type":"text","text":"system prompt"}]}}"#,
        );
        assert!(m.is_none());
    }

    #[test]
    fn keeps_tool_result_in_injected_turn() {
        // isMeta user 含 tool_result → 只留 tool_result，role=tool
        let m = decode_str(
            r#"{"type":"user","uuid":"u1","isMeta":true,"message":{"content":[{"type":"text","text":"prompt"},{"type":"tool_result","content":"result","is_error":false}]}}"#,
        )
        .unwrap();
        assert_eq!(m.role, TranscriptRole::Tool);
        assert_eq!(m.blocks.len(), 1);
        assert!(matches!(
            m.blocks[0],
            TranscriptBlock::ToolResult { .. }
        ));
    }

    #[test]
    fn maps_tool_result_only_user_to_tool() {
        let m = decode_str(
            r#"{"type":"user","uuid":"u1","message":{"content":[{"type":"tool_result","content":"r"}]}}"#,
        )
        .unwrap();
        assert_eq!(m.role, TranscriptRole::Tool);
    }

    #[test]
    fn skips_unknown_type_and_block() {
        // 未知 type → None
        assert!(decode_str(r#"{"type":"mode","uuid":"m1","message":{"content":"x"}}"#).is_none());
        // 未知 block → 该 block 丢弃，剩余有效 block 保留
        let m = decode_str(
            r#"{"type":"assistant","uuid":"a1","message":{"content":[{"type":"unknown","x":1},{"type":"text","text":"ok"}]}}"#,
        )
        .unwrap();
        assert_eq!(m.blocks.len(), 1);
    }

    #[test]
    fn skips_empty_content() {
        assert!(decode_str(r#"{"type":"user","uuid":"u1","message":{"content":"   "}}"#).is_none());
        assert!(decode_str(r#"{"type":"user","uuid":"u1","message":{"content":[]}}"#).is_none());
    }

    #[test]
    fn invalid_timestamp_yields_none() {
        assert!(parse_timestamp_ms("not-a-date").is_none());
        // 2026-08-22T09:21:18.108Z 的精确毫秒值（回归断言，防解析实现漂移）。
        assert_eq!(
            parse_timestamp_ms("2026-08-22T09:21:18.108Z"),
            Some(1787390478108)
        );
    }
}
