// claude_runtime 域类型：Claude hook 载荷反序列化 + 前端事件 payload。
// 与 shared/types.rs 的边界：shared/types.rs 收跨域共享类型；本域自含 hook/runtime
// 专属类型（HookPayload 为内部反序列化，不 specta 导出；payload/status/notification
// 经 build_specta_builder 显式注册导出）。

use serde::{Deserialize, Serialize};
use specta::Type;
// Number 用于把 i64 时间戳在 specta 导出时映射为 TS `number`（毫秒时间戳精度安全）。
use specta_typescript::Number;

// ============================================================
// Hook 载荷（T1.3 ingest 反序列化用，内部类型，不 specta 导出）
// ============================================================

/// Claude hooks 经 stdin 传给的 JSON 载荷。serde 容忍未知字段（默认忽略）、
/// 缺失字段（`#[serde(default)]` → 空串/None），损坏行由 ingest 层 skip。
/// `hook_event_name` 存原串（SessionStart/User/Assistant/Stop/Notification 等，
/// 具体事件名归一化在 ingest 层按需匹配），不做严格枚举以免未来事件名漂移。
#[derive(Debug, Clone, Deserialize)]
pub struct HookPayload {
    /// hook 事件名（如 "SessionStart" / "Stop" / "Notification"）。
    #[serde(default)]
    pub hook_event_name: String,
    /// 会话 ID（uuid）。SessionStart 时绑定。
    #[serde(default)]
    pub session_id: Option<String>,
    /// transcript JSONL 绝对路径。SessionStart 时绑定。
    #[serde(default)]
    pub transcript_path: Option<String>,
    /// 会话工作目录。
    #[serde(default)]
    pub cwd: Option<String>,
    /// 触发审批/通知的工具名（如 "Bash"）。
    #[serde(default)]
    pub tool_name: Option<String>,
    /// 工具入参（JSON 原文，类型不定故用 Value 宽容接收）。
    #[serde(default)]
    pub tool_input: Option<serde_json::Value>,
    /// 通知/提问原文（Notification 事件）。
    #[serde(default)]
    pub message: Option<String>,
}

// ============================================================
// 前端事件 payload（经 build_specta_builder 注册导出）
// ============================================================

/// 运行时状态机：hook 事件驱动（区别于 ClaudeSessionStatus 的会话轮询态）。
/// - Idle：会话空闲（Stop / SessionStart 初始）
/// - Working：正在生成（User 已提交 / Assistant 生成中）
/// - Waiting：等待用户输入（Notification 审批/提问）
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default, Serialize, Deserialize, Type)]
#[serde(rename_all = "lowercase")]
pub enum ClaudeRuntimeStatus {
    #[default]
    Idle,
    Working,
    Waiting,
}

/// Notification 载荷的结构化形态（审批/提问卡片数据源，T4.1 渲染）。
/// message 为通知/提问原文；审批类携带 tool_name/tool_input（权限确认上下文），
/// 自由提问类 tool_name 为空（选项列表在 message 内解析）。
#[derive(Clone, Debug, Serialize, Deserialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct ClaudeNotification {
    /// 通知/提问原文。
    pub message: String,
    /// 待审批工具名（如 "Bash"）；自由提问为 None。
    #[serde(default)]
    pub tool_name: Option<String>,
    /// 待审批工具入参（JSON 序列化字符串）；自由提问为 None。
    #[serde(default)]
    pub tool_input: Option<String>,
}

/// runtime 状态变更事件载荷（`claude-runtime:changed`）。ingest 归一化后 emit，
/// 前端 useClaudeRuntime 按 pane 过滤订阅。
#[derive(Clone, Serialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct ClaudeRuntimeChangedPayload {
    /// pane 锚点（issueId::paneId，store key）。
    pub pane: String,
    /// 运行时状态（idle/working/waiting）。
    pub status: ClaudeRuntimeStatus,
    /// 生成中的预览文本（assistant 实时增量）。
    pub preview_text: Option<String>,
    /// 审批/提问通知（waiting 态）。
    pub notification: Option<ClaudeNotification>,
    /// transcript JSONL 绝对路径（SessionStart 绑定，chat 视图定位用）。
    pub transcript_path: Option<String>,
    /// claude 会话 ID（resume 用）。
    pub claude_session_id: Option<String>,
    /// 最后更新时间（毫秒时间戳）。
    #[specta(type = Number)]
    pub last_updated_at: i64,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hook_payload_tolerates_unknown_and_missing_fields() {
        let json = r#"{
            "hook_event_name": "Notification",
            "session_id": "s1",
            "message": "approve Bash",
            "tool_name": "Bash",
            "tool_input": {"command": "ls"},
            "unknown_field": 123
        }"#;
        let p: HookPayload = serde_json::from_str(json).unwrap();
        assert_eq!(p.hook_event_name, "Notification");
        assert_eq!(p.session_id.as_deref(), Some("s1"));
        assert_eq!(p.message.as_deref(), Some("approve Bash"));
        assert_eq!(p.tool_name.as_deref(), Some("Bash"));
        assert!(p.tool_input.is_some());
        assert!(p.transcript_path.is_none());
        assert!(p.cwd.is_none());
    }

    #[test]
    fn hook_payload_missing_all_optionals() {
        let p: HookPayload = serde_json::from_str(r#"{"hook_event_name":"Stop"}"#).unwrap();
        assert_eq!(p.hook_event_name, "Stop");
        assert!(p.session_id.is_none());
        assert!(p.transcript_path.is_none());
        assert!(p.cwd.is_none());
        assert!(p.tool_name.is_none());
        assert!(p.tool_input.is_none());
        assert!(p.message.is_none());
    }

    #[test]
    fn hook_payload_missing_event_name_defaults_empty() {
        let p: HookPayload = serde_json::from_str(r#"{"session_id":"s1"}"#).unwrap();
        assert_eq!(p.hook_event_name, "");
    }

    #[test]
    fn status_serializes_lowercase() {
        assert_eq!(
            serde_json::to_string(&ClaudeRuntimeStatus::Working).unwrap(),
            "\"working\""
        );
        assert_eq!(
            serde_json::to_string(&ClaudeRuntimeStatus::Idle).unwrap(),
            "\"idle\""
        );
    }
}
