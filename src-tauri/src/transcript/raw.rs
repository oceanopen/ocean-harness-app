// 反序列化单行 transcript JSONL 为结构化 RawLine（terminal_chat T1.3）。
//
// 容错 schema 漂移：未知字段 serde 默认忽略；content 用 untagged 枚举兼容
// string / array 两种形态；block 字段全 Option + default（不同 block type 各自
// 只填自己的字段，其余为 None）。损坏 JSON 由调用方（reader）silently skip。

use serde::Deserialize;

/// transcript JSONL 单行（仅声明解析所需字段，未知字段 serde 忽略）。
#[derive(Clone, Debug, Deserialize)]
pub struct RawLine {
    /// 顶层 type：user / assistant / system / mode / ...
    #[serde(rename = "type")]
    pub type_: String,
    /// 记录 uuid（消息唯一标识）。
    #[serde(default)]
    pub uuid: Option<String>,
    /// 时间戳（ISO8601 字符串）。
    #[serde(default)]
    pub timestamp: Option<String>,
    /// 注入标记（系统提示注入，非用户真话）。JSON 顶层为 camelCase。
    #[serde(default, rename = "isMeta")]
    pub is_meta: Option<bool>,
    /// 注入标记（合成消息）。
    #[serde(default, rename = "isSynthetic")]
    pub is_synthetic: Option<bool>,
    /// 注入标记（压缩摘要）。
    #[serde(default, rename = "isCompactSummary")]
    pub is_compact_summary: Option<bool>,
    /// 消息体（含 content）。
    #[serde(default)]
    pub message: Option<RawMessage>,
}

/// 消息体：仅声明 content 字段（id/model 等当前用不到）。
#[derive(Clone, Debug, Deserialize)]
pub struct RawMessage {
    #[serde(default)]
    pub content: Option<RawContent>,
}

/// content 形态：user 行可能是 string 或 array；assistant 行恒 array；system 行恒 None。
#[derive(Clone, Debug, Deserialize)]
#[serde(untagged)]
pub enum RawContent {
    String(String),
    Array(Vec<RawBlock>),
}

/// 单个 content block（字段并集 + 全 Option，按 type 分支取用）。
#[derive(Clone, Debug, Deserialize)]
pub struct RawBlock {
    #[serde(rename = "type")]
    pub type_: String,
    /// text / thinking 的正文。
    #[serde(default)]
    pub text: Option<String>,
    /// thinking block 的正文（优先于 text）。
    #[serde(default)]
    pub thinking: Option<String>,
    /// tool_use 的工具名。
    #[serde(default)]
    pub name: Option<String>,
    /// tool_use 的参数（任意 JSON）。
    #[serde(default)]
    pub input: Option<serde_json::Value>,
    /// tool_result 的结果内容（string 或 array，任意 JSON）。
    #[serde(default)]
    pub content: Option<serde_json::Value>,
    /// tool_result 是否错误。
    #[serde(default)]
    pub is_error: Option<bool>,
    /// image 的 url。
    #[serde(default)]
    pub url: Option<String>,
    /// image 的 path。
    #[serde(default)]
    pub path: Option<String>,
    /// image 的 alt。
    #[serde(default)]
    pub alt: Option<String>,
}

/// 解析单行 JSONL 为 RawLine。
/// 调用方负责处理 Err（损坏行 / schema 漂移），上层 silently skip。
pub fn parse(content: &str) -> Result<RawLine, serde_json::Error> {
    serde_json::from_str(content)
}

#[cfg(test)]
mod tests {
    use super::*;

    // 占位样例：含 string/array 两种 content 形态、注入标记、tool_use 参数，
    // 用于回归 Claude Code 写入 schema 漂移时立即报警。
    const USER_STRING: &str = r#"{"type":"user","uuid":"u1","timestamp":"2026-08-22T09:21:18.108Z","sessionId":"s1","isMeta":false,"isSidechain":false,"message":{"content":"hi"}}"#;
    const USER_ARRAY: &str = r#"{"type":"user","uuid":"u2","isMeta":true,"message":{"content":[{"type":"text","text":"system prompt"}]}}"#;
    const ASSISTANT: &str = r#"{"type":"assistant","uuid":"a1","session_id":"s1","message":{"content":[{"type":"thinking","thinking":"...","signature":"x"},{"type":"tool_use","name":"Read","id":"t1","input":{"path":"a.ts"}}]}}"#;

    #[test]
    fn parses_user_string() {
        let r = parse(USER_STRING).unwrap();
        assert_eq!(r.type_, "user");
        assert_eq!(r.uuid.as_deref(), Some("u1"));
        assert_eq!(r.is_meta, Some(false));
        match r.message.unwrap().content.unwrap() {
            RawContent::String(s) => assert_eq!(s, "hi"),
            _ => panic!("expected string content"),
        }
    }

    #[test]
    fn parses_user_array_blocks() {
        let r = parse(USER_ARRAY).unwrap();
        assert_eq!(r.is_meta, Some(true));
        match r.message.unwrap().content.unwrap() {
            RawContent::Array(blocks) => {
                assert_eq!(blocks.len(), 1);
                assert_eq!(blocks[0].type_, "text");
                assert_eq!(blocks[0].text.as_deref(), Some("system prompt"));
            }
            _ => panic!("expected array content"),
        }
    }

    #[test]
    fn parses_assistant_tool_use_input() {
        let r = parse(ASSISTANT).unwrap();
        match r.message.unwrap().content.unwrap() {
            RawContent::Array(blocks) => {
                assert_eq!(blocks.len(), 2);
                assert_eq!(blocks[1].type_, "tool_use");
                assert_eq!(blocks[1].name.as_deref(), Some("Read"));
                assert!(blocks[1].input.is_some());
            }
            _ => panic!("expected array content"),
        }
    }

    #[test]
    fn ignores_unknown_fields_and_missing_message() {
        // 未知字段（sessionId/session_id）忽略；无 message 的行（system/mode）parse 成功、字段为 None。
        let r = parse(r#"{"type":"mode","unknown":"x"}"#).unwrap();
        assert_eq!(r.type_, "mode");
        assert!(r.message.is_none());
        assert!(r.uuid.is_none());
    }

    #[test]
    fn rejects_invalid_json() {
        assert!(parse("not json").is_err());
    }
}
