// 反序列化 `~/.claude/sessions/<pid>.json` 为结构化数据。
//
// Claude Code 实际写入的 json 还含 procStart/version/kind/entrypoint 等字段，
// 这些当前业务用不到，不声明避免无谓反序列化开销（serde 默认忽略未知字段）。

use serde::{Deserialize, Serialize};

/// `~/.claude/sessions/<pid>.json` 的反序列化结构。
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct RawSessionFile {
    /// Claude Code 进程 pid（也是文件名 stem）。
    pub pid: u32,
    /// 会话 ID（uuid）。
    #[serde(rename = "sessionId")]
    pub session_id: String,
    /// 工作目录绝对路径。
    pub cwd: String,
    /// 启动时间（毫秒时间戳）。
    #[serde(rename = "startedAt")]
    pub started_at: i64,
    /// 状态字符串，原始值如 "busy" / "waiting" / "idle"。
    pub status: String,
    /// 最后状态更新时间（毫秒时间戳）。
    #[serde(rename = "updatedAt")]
    pub updated_at: i64,
}

/// 解析 json 字符串为 RawSessionFile。
/// 调用方负责处理 Err（文件损坏 / schema 漂移），上层通常 silently skip。
pub fn parse(content: &str) -> Result<RawSessionFile, serde_json::Error> {
    serde_json::from_str(content)
}

#[cfg(test)]
mod tests {
    use super::*;

    // 占位样例：保留 procStart/version/peerProtocol/kind/entrypoint 等额外字段，
    // 用于回归 Claude Code 写入 schema 漂移时立即报警。
    const SAMPLE: &str = r#"{"pid":1,"sessionId":"00000000-0000-0000-0000-000000000001","cwd":"/home/user/project","startedAt":1700000000000,"procStart":"Mon Jan 1 00:00:00 2024","version":"2.1.153","peerProtocol":1,"kind":"interactive","entrypoint":"cli","status":"idle","updatedAt":1700000000000}"#;

    #[test]
    fn parses_sample_session_file() {
        let s = parse(SAMPLE).expect("sample must parse");
        assert_eq!(s.pid, 1);
        assert_eq!(
            s.session_id,
            "00000000-0000-0000-0000-000000000001"
        );
        assert_eq!(s.cwd, "/home/user/project");
        assert_eq!(s.started_at, 1700000000000);
        assert_eq!(s.status, "idle");
        assert_eq!(s.updated_at, 1700000000000);
    }

    #[test]
    fn ignores_extra_fields() {
        // peerProtocol / version 等字段当前不声明，serde 默认忽略。
        let s = parse(SAMPLE).unwrap();
        assert_eq!(s.pid, 1);
    }

    #[test]
    fn rejects_invalid_json() {
        assert!(parse("not json").is_err());
    }
}
