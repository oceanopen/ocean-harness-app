/*
 * 直读 `~/.claude/projects/<slug>/<sessionId>.jsonl` 会话转录（应用不存消息内容）。
 * R4：宽松解析 + 失败降级空态；字节偏移增量读；slug 未命中按 sessionId 兜底扫描，容忍规则漂移与文件轮换瞬时不可见。
 */
use std::fs::File;
use std::io::{BufRead, BufReader, Seek, SeekFrom};
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use specta_typescript::Number;

// 单次 command 返回的条目上限（超出走 hasMore 续读）。
const ENTRY_CAP_PER_CALL: usize = 2000;
// 尾读窗口：取最新消息时间只需文件末尾若干完整行。
const TAIL_WINDOW_BYTES: u64 = 64 * 1024;
// tool_result 单块文本字节上限（转录体积大头）。
const TOOL_RESULT_MAX_BYTES: usize = 2048;
// toolCall 摘要单行字符上限。
const TOOL_SUMMARY_MAX_CHARS: usize = 120;

// 消息条目类别（与 ACP 视图 kind 语义同构，T5.3 两模式共用渲染）。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "camelCase")]
pub enum ClaudeEntryKind {
    User,
    AgentMessage,
    AgentThought,
    ToolCall,
    ToolResult,
}

// 单条会话消息。toolCall 条目仅 toolName/toolSummary 有值；toolResult 正文在 text（已截断）。
#[derive(Clone, Debug, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "camelCase")]
pub struct ClaudeSessionEntry {
    // 行内 uuid（多块行追加 ":<块序号>"）；缺省回落 "off-<行字节偏移>"，跨增量读稳定。
    pub entry_id: String,
    pub kind: ClaudeEntryKind,
    // ISO 8601 字符串原样透出；缺失为空串。
    pub timestamp: String,
    pub text: String,
    pub tool_name: String,
    pub tool_summary: String,
}

// 全量/增量读取结果。latestTimestamp 为本次扫描段最后一条消息时间（offset=0 即全文件最新）。
#[derive(Clone, Debug, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "camelCase")]
pub struct ClaudeSessionMessages {
    pub entries: Vec<ClaudeSessionEntry>,
    #[specta(type = Number)]
    pub next_offset: u64,
    pub has_more: bool,
    pub latest_timestamp: String,
    // true = 文件缺失/读取失败（区别于真空会话），前端可呈现「暂无本地历史」。
    pub degraded: bool,
}

// 尾读结果：列表行最新消息时间，避免全量解析。
#[derive(Clone, Debug, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "camelCase")]
pub struct ClaudeSessionLatest {
    pub timestamp: String,
    pub degraded: bool,
}

impl ClaudeSessionMessages {
    fn degraded() -> Self {
        Self {
            entries: vec![],
            next_offset: 0,
            has_more: false,
            latest_timestamp: String::new(),
            degraded: true,
        }
    }
}

/*
 * cwd → projects 目录 slug：保留字母数字/-/_，其余替换 '-'（对齐 CLI 写盘实证规则）。
 * 空结果回落 "unknown"（CLI 同款兜底）。
 */
fn project_slug(cwd: &str) -> String {
    let s: String = cwd
        .chars()
        .map(|c| {
            if c.is_ascii_alphanumeric() || c == '-' || c == '_' {
                c
            } else {
                '-'
            }
        })
        .collect();
    if s.is_empty() {
        "unknown".to_string()
    } else {
        s
    }
}

// `~/.claude/projects`，home_dir 探测失败返回 None（对齐 discover.rs）。
fn claude_projects_dir() -> Option<PathBuf> {
    dirs::home_dir().map(|h| h.join(".claude").join("projects"))
}

/*
 * 转录文件定位：slug 主路径未命中时在 projects 一层子目录按文件名兜底扫描；
 * 都未命中返回 None（文件确实不存在 / CLI 从未落盘）。
 */
fn resolve_transcript_in(projects_dir: &Path, cwd: &str, session_id: &str) -> Option<PathBuf> {
    let file_name = format!("{}.jsonl", session_id);
    let primary = projects_dir
        .join(project_slug(cwd))
        .join(&file_name);
    if primary.is_file() {
        return Some(primary);
    }
    for entry in std::fs::read_dir(projects_dir).ok()?.flatten() {
        let candidate = entry.path().join(&file_name);
        if candidate.is_file() {
            return Some(candidate);
        }
    }
    None
}

fn resolve_transcript_path(cwd: &str, session_id: &str) -> Option<PathBuf> {
    let dir = claude_projects_dir()?;
    resolve_transcript_in(&dir, cwd, session_id)
}

// 折叠为单行并按字符上限截断（字符边界安全）。
fn one_line(s: &str, max_chars: usize) -> String {
    let flat: String = s
        .chars()
        .map(|c| {
            if c == '\n' || c == '\r' {
                ' '
            } else {
                c
            }
        })
        .collect();
    flat.chars().take(max_chars).collect()
}

// 按字节上限截断并回落到 UTF-8 字符边界。
fn truncate_utf8(s: &str, max_bytes: usize) -> String {
    if s.len() <= max_bytes {
        return s.to_string();
    }
    let mut end = max_bytes;
    while end > 0 && !s.is_char_boundary(end) {
        end -= 1;
    }
    s[..end].to_string()
}

// toolCall 摘要：优先取常见语义键，否则压缩整个 input JSON。
fn tool_summary_of(input: Option<&serde_json::Value>) -> String {
    const KEYS: [&str; 8] = [
        "command",
        "file_path",
        "path",
        "pattern",
        "url",
        "query",
        "prompt",
        "description",
    ];
    let raw = match input {
        Some(v) => KEYS
            .iter()
            .find_map(|k| v.get(k).and_then(|x| x.as_str()))
            .map(|s| s.to_string())
            .unwrap_or_else(|| v.to_string()),
        None => String::new(),
    };
    one_line(&raw, TOOL_SUMMARY_MAX_CHARS)
}

// tool_result 正文：字符串或块数组（text 拼接，image 跳过），字节截断。
fn tool_result_text(block: &serde_json::Value) -> String {
    match block.get("content") {
        Some(serde_json::Value::String(s)) => truncate_utf8(s, TOOL_RESULT_MAX_BYTES),
        Some(serde_json::Value::Array(blocks)) => {
            let mut out = String::new();
            for b in blocks {
                if b.get("type").and_then(|t| t.as_str()) != Some("text") {
                    continue;
                }
                if let Some(t) = b.get("text").and_then(|t| t.as_str()) {
                    if !out.is_empty() {
                        out.push('\n');
                    }
                    out.push_str(t);
                    if out.len() >= TOOL_RESULT_MAX_BYTES {
                        break;
                    }
                }
            }
            truncate_utf8(&out, TOOL_RESULT_MAX_BYTES)
        }
        _ => String::new(),
    }
}

/*
 * 单行宽松解析为若干条目：只认 user/assistant 消息行；sidechain（子 agent）与 isMeta 行跳过；
 * 坏行静默跳过。assistant 一行的 thinking/text/tool_use 块各成一条目（顺序保持）。
 */
fn parse_line(line: &str, line_offset: u64) -> Vec<ClaudeSessionEntry> {
    let Ok(v) = serde_json::from_str::<serde_json::Value>(line) else {
        return vec![];
    };
    let truthy = |key: &str| {
        v.get(key)
            .and_then(|b| b.as_bool())
            .unwrap_or(false)
    };
    if truthy("isSidechain") || truthy("isMeta") {
        return vec![];
    }
    let id_base = v
        .get("uuid")
        .and_then(|u| u.as_str())
        .map(|s| s.to_string())
        .unwrap_or_else(|| format!("off-{}", line_offset));
    let timestamp = v
        .get("timestamp")
        .and_then(|t| t.as_str())
        .unwrap_or("")
        .to_string();
    let message = v.get("message");
    let make = |i: usize,
                kind: ClaudeEntryKind,
                entry: &dyn Fn(&ClaudeSessionEntry) -> ClaudeSessionEntry| {
        let base = ClaudeSessionEntry {
            entry_id: format!("{}:{}", id_base, i),
            kind,
            timestamp: timestamp.clone(),
            text: String::new(),
            tool_name: String::new(),
            tool_summary: String::new(),
        };
        entry(&base)
    };
    match v.get("type").and_then(|t| t.as_str()) {
        Some("user") => match message.and_then(|m| m.get("content")) {
            Some(serde_json::Value::String(s)) => {
                vec![make(0, ClaudeEntryKind::User, &|e| {
                    ClaudeSessionEntry {
                        text: s.clone(),
                        ..e.clone()
                    }
                })]
            }
            Some(serde_json::Value::Array(blocks)) => {
                let mut out = vec![];
                let mut text_acc = String::new();
                for block in blocks {
                    match block.get("type").and_then(|t| t.as_str()) {
                        Some("text") => {
                            if let Some(t) = block.get("text").and_then(|t| t.as_str()) {
                                if !text_acc.is_empty() {
                                    text_acc.push('\n');
                                }
                                text_acc.push_str(t);
                            }
                        }
                        Some("tool_result") => {
                            if !text_acc.is_empty() {
                                out.push(make(out.len(), ClaudeEntryKind::User, &|e| {
                                    ClaudeSessionEntry {
                                        text: text_acc.clone(),
                                        ..e.clone()
                                    }
                                }));
                                text_acc.clear();
                            }
                            let body = tool_result_text(block);
                            out.push(make(
                                out.len(),
                                ClaudeEntryKind::ToolResult,
                                &|e| ClaudeSessionEntry {
                                    text: body.clone(),
                                    ..e.clone()
                                },
                            ));
                        }
                        _ => {}
                    }
                }
                if !text_acc.is_empty() {
                    out.push(make(out.len(), ClaudeEntryKind::User, &|e| {
                        ClaudeSessionEntry {
                            text: text_acc.clone(),
                            ..e.clone()
                        }
                    }));
                }
                out
            }
            _ => vec![],
        },
        Some("assistant") => {
            let Some(serde_json::Value::Array(blocks)) = message.and_then(|m| m.get("content"))
            else {
                return vec![];
            };
            let mut out = vec![];
            for block in blocks {
                match block.get("type").and_then(|t| t.as_str()) {
                    Some("thinking") => {
                        let t = block
                            .get("thinking")
                            .and_then(|t| t.as_str())
                            .unwrap_or("")
                            .to_string();
                        out.push(make(
                            out.len(),
                            ClaudeEntryKind::AgentThought,
                            &|e| ClaudeSessionEntry {
                                text: t.clone(),
                                ..e.clone()
                            },
                        ));
                    }
                    Some("text") => {
                        let t = block
                            .get("text")
                            .and_then(|t| t.as_str())
                            .unwrap_or("")
                            .to_string();
                        out.push(make(
                            out.len(),
                            ClaudeEntryKind::AgentMessage,
                            &|e| ClaudeSessionEntry {
                                text: t.clone(),
                                ..e.clone()
                            },
                        ));
                    }
                    Some("tool_use") => {
                        let name = block
                            .get("name")
                            .and_then(|t| t.as_str())
                            .unwrap_or("")
                            .to_string();
                        let summary = tool_summary_of(block.get("input"));
                        out.push(make(out.len(), ClaudeEntryKind::ToolCall, &|e| {
                            ClaudeSessionEntry {
                                tool_name: name.clone(),
                                tool_summary: summary.clone(),
                                ..e.clone()
                            }
                        }));
                    }
                    _ => {}
                }
            }
            out
        }
        _ => vec![],
    }
}

/*
 * 从字节偏移起读消息条目（read_until 精确计行字节，跨调用续读无损）。
 * offset 超出文件长度（文件被轮换/重写变短）归零全读自愈。
 * cap 为条目上限（正式路径传 ENTRY_CAP_PER_CALL，测试注入小值）。
 */
fn read_messages_from(
    path: &Path,
    offset: u64,
    cap: usize,
) -> std::io::Result<ClaudeSessionMessages> {
    let mut file = File::open(path)?;
    let len = file.metadata()?.len();
    let start = if offset > len {
        0
    } else {
        offset
    };
    file.seek(SeekFrom::Start(start))?;
    let mut reader = BufReader::new(file);
    let mut buf = Vec::with_capacity(4096);
    let mut pos = start;
    let mut entries: Vec<ClaudeSessionEntry> = vec![];
    let mut latest = String::new();
    let mut has_more = false;
    loop {
        if entries.len() >= cap {
            has_more = true;
            break;
        }
        buf.clear();
        let n = reader.read_until(b'\n', &mut buf)?;
        if n == 0 {
            break;
        }
        if buf.last() != Some(&b'\n') {
            // 末段无换行符：行可能仍在写入，本轮不消费不前移 offset。
            break;
        }
        let line_start = pos;
        pos += n as u64;
        let line = String::from_utf8_lossy(&buf);
        let line = line.trim_end_matches(['\n', '\r']);
        if line.is_empty() {
            continue;
        }
        for e in parse_line(line, line_start) {
            latest = e.timestamp.clone();
            entries.push(e);
        }
    }
    Ok(ClaudeSessionMessages {
        entries,
        next_offset: pos,
        has_more,
        latest_timestamp: latest,
        degraded: false,
    })
}

/*
 * 尾部截读最新消息时间：窗口起点落在行中间时首行视为残行丢弃。
 * 窗口内无消息行（极端：单条超长行）返回空串空态。
 */
fn read_latest_from(path: &Path) -> std::io::Result<ClaudeSessionLatest> {
    let mut file = File::open(path)?;
    let len = file.metadata()?.len();
    if len == 0 {
        return Ok(ClaudeSessionLatest {
            timestamp: String::new(),
            degraded: false,
        });
    }
    let start = len.saturating_sub(TAIL_WINDOW_BYTES);
    file.seek(SeekFrom::Start(start))?;
    let mut reader = BufReader::new(file);
    let mut buf = Vec::with_capacity(4096);
    let mut skip_partial = start > 0;
    let mut latest = String::new();
    loop {
        buf.clear();
        let n = reader.read_until(b'\n', &mut buf)?;
        if n == 0 {
            break;
        }
        if skip_partial {
            skip_partial = false;
            continue;
        }
        let line = String::from_utf8_lossy(&buf);
        let line = line.trim_end_matches(['\n', '\r']);
        if line.is_empty() {
            continue;
        }
        if let Some(e) = parse_line(line, 0).last() {
            latest = e.timestamp.clone();
        }
    }
    Ok(ClaudeSessionLatest {
        timestamp: latest,
        degraded: false,
    })
}

// 全量/增量读取会话消息。文件缺失或读取失败一律降级空态（R4），Err 仅留给空参数。
#[tauri::command]
#[specta::specta]
pub fn get_claude_session_messages(
    cwd: String,
    session_id: String,
    // wire 用 u32（specta 禁 BigInt 型参数），内部拓宽 u64；偏移上限 4GB 对转录足够。
    offset: Option<u32>,
) -> Result<ClaudeSessionMessages, String> {
    if cwd.trim().is_empty() || session_id.trim().is_empty() {
        return Err("cwd 与 sessionId 不能为空".to_string());
    }
    let Some(path) = resolve_transcript_path(&cwd, &session_id) else {
        return Ok(ClaudeSessionMessages::degraded());
    };
    match read_messages_from(
        &path,
        offset.map(|o| o as u64).unwrap_or(0),
        ENTRY_CAP_PER_CALL,
    ) {
        Ok(m) => Ok(m),
        Err(e) => {
            log::warn!(
                "[sessions] 读取转录失败 {}: {}",
                path.display(),
                e
            );
            Ok(ClaudeSessionMessages::degraded())
        }
    }
}

// 尾读会话最新消息时间（列表行展示用）。降级语义同全量读。
#[tauri::command]
#[specta::specta]
pub fn get_claude_session_latest(
    cwd: String,
    session_id: String,
) -> Result<ClaudeSessionLatest, String> {
    if cwd.trim().is_empty() || session_id.trim().is_empty() {
        return Err("cwd 与 sessionId 不能为空".to_string());
    }
    let Some(path) = resolve_transcript_path(&cwd, &session_id) else {
        return Ok(ClaudeSessionLatest {
            timestamp: String::new(),
            degraded: true,
        });
    };
    match read_latest_from(&path) {
        Ok(m) => Ok(m),
        Err(e) => {
            log::warn!(
                "[sessions] 尾读转录失败 {}: {}",
                path.display(),
                e
            );
            Ok(ClaudeSessionLatest {
                timestamp: String::new(),
                degraded: true,
            })
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    fn temp_file(name: &str, content: &[u8]) -> PathBuf {
        let p = std::env::temp_dir().join(format!(
            "ocean-jsonl-test-{}-{}",
            std::process::id(),
            name
        ));
        let mut f = File::create(&p).unwrap();
        f.write_all(content).unwrap();
        p
    }

    #[test]
    fn slug_keeps_word_chars_and_replaces_separators() {
        assert_eq!(
            project_slug("/Users/foo/My_Dir-a"),
            "-Users-foo-My_Dir-a"
        );
        assert_eq!(project_slug(""), "unknown");
    }

    #[test]
    fn resolves_primary_then_fallback_scan() {
        let dir = std::env::temp_dir().join(format!(
            "ocean-jsonl-projects-{}",
            std::process::id()
        ));
        let _ = std::fs::remove_dir_all(&dir);
        let sid = "11111111-1111-1111-1111-111111111111";
        let odd = dir.join("some-odd-slug");
        std::fs::create_dir_all(&odd).unwrap();
        std::fs::write(odd.join(format!("{}.jsonl", sid)), "{}\n").unwrap();
        // slug 主路径未命中 → 兜底扫描命中。
        assert!(resolve_transcript_in(&dir, "/not/matching", sid).is_some());
        assert!(
            resolve_transcript_in(
                &dir,
                "/not/matching",
                "22222222-2222-2222-2222-222222222222"
            )
            .is_none()
        );
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn parses_user_string_and_blocks() {
        let lines = concat!(
            r#"{"type":"user","timestamp":"2026-01-01T00:00:01.000Z","uuid":"u1","message":{"role":"user","content":"你好"}}"#,
            "\n",
            r#"{"type":"user","timestamp":"2026-01-01T00:00:02.000Z","uuid":"u2","message":{"content":[{"type":"text","text":"a"},{"type":"tool_result","content":"result-body"}]}}"#,
            "\n"
        );
        let e0 = &parse_line(lines.lines().next().unwrap(), 0)[0];
        assert_eq!(e0.kind, ClaudeEntryKind::User);
        assert_eq!(e0.text, "你好");
        assert_eq!(e0.timestamp, "2026-01-01T00:00:01.000Z");
        let rest = parse_line(lines.lines().nth(1).unwrap(), 100);
        assert_eq!(rest.len(), 2);
        assert_eq!(rest[0].kind, ClaudeEntryKind::User);
        assert_eq!(rest[0].text, "a");
        assert_eq!(rest[1].kind, ClaudeEntryKind::ToolResult);
        assert_eq!(rest[1].text, "result-body");
        assert_eq!(rest[1].entry_id, "u2:1");
    }

    #[test]
    fn parses_assistant_blocks_in_order() {
        let line = r#"{"type":"assistant","timestamp":"2026-01-01T00:00:03.000Z","uuid":"a1","message":{"content":[{"type":"thinking","thinking":"想"},{"type":"text","text":"答"},{"type":"tool_use","name":"Bash","input":{"command":"pnpm web:build\n--watch"}}]}}"#;
        let entries = parse_line(line, 0);
        assert_eq!(entries.len(), 3);
        assert_eq!(entries[0].kind, ClaudeEntryKind::AgentThought);
        assert_eq!(entries[0].text, "想");
        assert_eq!(entries[1].kind, ClaudeEntryKind::AgentMessage);
        assert_eq!(entries[1].text, "答");
        assert_eq!(entries[2].kind, ClaudeEntryKind::ToolCall);
        assert_eq!(entries[2].tool_name, "Bash");
        assert_eq!(entries[2].tool_summary, "pnpm web:build --watch");
        assert_eq!(entries[0].entry_id, "a1:0");
    }

    #[test]
    fn skips_meta_sidechain_and_bad_lines() {
        assert!(parse_line(r#"{"type":"mode","sessionId":"x"}"#, 0).is_empty());
        assert!(
            parse_line(
                r#"{"type":"user","isSidechain":true,"message":{"content":"s"}}"#,
                0
            )
            .is_empty()
        );
        assert!(parse_line(r#"{"type":"user","isMeta":true}"#, 0).is_empty());
        assert!(parse_line("not json", 0).is_empty());
    }

    #[test]
    fn truncates_tool_result_bytes() {
        let long = "a".repeat(3000);
        let line = format!(
            r#"{{"type":"user","uuid":"t1","message":{{"content":[{{"type":"tool_result","content":"{}"}}]}}}}"#,
            long
        );
        let e = &parse_line(&line, 0)[0];
        assert_eq!(e.kind, ClaudeEntryKind::ToolResult);
        assert_eq!(e.text.len(), TOOL_RESULT_MAX_BYTES);
    }

    #[test]
    fn reads_incrementally_and_self_heals_offset() {
        let l1 = r#"{"type":"user","timestamp":"2026-01-01T00:00:01.000Z","uuid":"u1","message":{"content":"one"}}"#;
        let l2 = r#"{"type":"assistant","timestamp":"2026-01-01T00:00:02.000Z","uuid":"a1","message":{"content":[{"type":"text","text":"two"}]}}"#;
        let p = temp_file("incr", format!("{}\n{}\n", l1, l2).as_bytes());
        let full = read_messages_from(&p, 0, ENTRY_CAP_PER_CALL).unwrap();
        assert_eq!(full.entries.len(), 2);
        assert!(!full.has_more);
        assert_eq!(full.latest_timestamp, "2026-01-01T00:00:02.000Z");
        // next_offset 应为文件长度（读到 EOF）。
        assert_eq!(
            full.next_offset,
            std::fs::metadata(&p).unwrap().len()
        );
        // 续读：无新内容。
        let again = read_messages_from(&p, full.next_offset, ENTRY_CAP_PER_CALL).unwrap();
        assert_eq!(again.entries.len(), 0);
        // offset 越界自愈为全读。
        let healed = read_messages_from(&p, 999_999, ENTRY_CAP_PER_CALL).unwrap();
        assert_eq!(healed.entries.len(), 2);
        // 单次上限：cap=1 时首条即截断并标记 hasMore，续读取余。
        let capped = read_messages_from(&p, 0, 1).unwrap();
        assert_eq!(capped.entries.len(), 1);
        assert!(capped.has_more);
        let rest = read_messages_from(&p, capped.next_offset, ENTRY_CAP_PER_CALL).unwrap();
        assert_eq!(rest.entries.len(), 1);
        let _ = std::fs::remove_file(&p);
    }

    #[test]
    fn partial_tail_line_not_consumed() {
        let l1 = r#"{"type":"user","timestamp":"2026-01-01T00:00:01.000Z","uuid":"u1","message":{"content":"one"}}"#;
        let l2 = r#"{"type":"assistant","timestamp":"2026-01-01T00:00:02.000Z","uuid":"a1","message":{"content":[{"type":"text","text":"two"}]}}"#;
        let p = temp_file(
            "partial",
            format!("{}\n{}", l1, &l2[..40]).as_bytes(),
        );
        let first = read_messages_from(&p, 0, ENTRY_CAP_PER_CALL).unwrap();
        assert_eq!(first.entries.len(), 1);
        assert_eq!(first.next_offset, l1.len() as u64 + 1);
        // 写入方补完整行后，同一 offset 续读拿到该条目（撕行不丢数据）。
        let mut f = std::fs::OpenOptions::new()
            .append(true)
            .open(&p)
            .unwrap();
        writeln!(f, "{}", &l2[40..]).unwrap();
        let second = read_messages_from(&p, first.next_offset, ENTRY_CAP_PER_CALL).unwrap();
        assert_eq!(second.entries.len(), 1);
        assert_eq!(
            second.entries[0].timestamp,
            "2026-01-01T00:00:02.000Z"
        );
        let _ = std::fs::remove_file(&p);
    }

    #[test]
    fn latest_reads_tail_and_degrades() {
        let l1 = r#"{"type":"user","timestamp":"2026-01-01T00:00:01.000Z","uuid":"u1","message":{"content":"one"}}"#;
        let l2 = r#"{"type":"assistant","timestamp":"2026-01-01T00:00:09.000Z","uuid":"a1","message":{"content":[{"type":"text","text":"two"}]}}"#;
        let p = temp_file("tail", format!("{}\n{}\n", l1, l2).as_bytes());
        let r = read_latest_from(&p).unwrap();
        assert!(!r.degraded);
        assert_eq!(r.timestamp, "2026-01-01T00:00:09.000Z");
        let _ = std::fs::remove_file(&p);
        // command 层：文件不存在降级空态，空参数 Err。
        let missing = get_claude_session_latest(
            "/definitely/not/exist".into(),
            "00000000-0000-0000-0000-000000000000".into(),
        )
        .unwrap();
        assert!(missing.degraded);
        assert!(get_claude_session_latest("".into(), "".into()).is_err());
        // 空文件既存在也可读：非降级，仅无时间。
        let empty = temp_file("tail-empty", b"");
        let r = read_latest_from(&empty).unwrap();
        assert!(!r.degraded);
        assert_eq!(r.timestamp, "");
        let _ = std::fs::remove_file(&empty);
    }
}
