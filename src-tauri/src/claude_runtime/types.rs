// claude_runtime 域类型：Claude hook 载荷反序列化 + 前端事件 payload。
// 与 shared/types.rs 的边界：shared/types.rs 收跨域共享类型；本域自含 hook/runtime
// 专属类型（HookPayload 为内部反序列化，不 specta 导出；payload 经
// build_specta_builder 显式注册导出）。
//
// chat 模式退役裁剪（2026-08）：状态机/预览/通知（ClaudeRuntimeStatus/
// ClaudeNotification 与 tool/message/delta 等事件字段）随 chat 前端删除；
// 保留会话绑定链（SessionStart → claude_session_id/transcript_path → resume）。

use serde::{Deserialize, Serialize};
use specta::Type;
// Number 用于把 i64 时间戳在 specta 导出时映射为 TS `number`（毫秒时间戳精度安全）。
use specta_typescript::Number;

// ============================================================
// Hook 载荷（T1.3 ingest 反序列化用，内部类型，不 specta 导出）
// ============================================================

/// Claude hooks 经 stdin 传给的 JSON 载荷。serde 容忍未知字段（默认忽略——
/// 已删除的历史字段与未来新字段同样透传）、缺失字段（`#[serde(default)]` →
/// 空串/None），损坏行由 ingest 层 skip。`hook_event_name` 存原串（chat 退役
/// 后仅消费 SessionStart，其余事件名自然落 Drop），不做严格枚举以免事件名漂移。
#[derive(Debug, Clone, Deserialize)]
pub struct HookPayload {
    /// hook 事件名（如 "SessionStart"）。
    #[serde(default)]
    pub hook_event_name: String,
    /// 会话 ID（uuid）。SessionStart 时绑定（resume 用）。
    #[serde(default)]
    pub session_id: Option<String>,
    /// transcript JSONL 绝对路径。SessionStart 时绑定。
    #[serde(default)]
    pub transcript_path: Option<String>,
    /// 代际标（hook 脚本注入，T1.3 围栏用——claude 自产载荷不含此字段）。
    #[serde(default)]
    pub launch_token: Option<String>,
    /// 子代理标（Task 子进程事件非空）。带 agent_id 的 SessionStart 忽略
    /// （防子代理抢占 pane 绑定，对齐 orca normalizeClaudeEvent）。
    #[serde(default)]
    pub agent_id: Option<serde_json::Value>,
}

// ============================================================
// 前端事件 payload（经 build_specta_builder 注册导出）
// ============================================================

/// runtime 状态变更事件载荷（`claude-runtime:changed`）。ingest 归一化后 emit，
/// 前端 useClaudeRunning latch 按 pane 过滤订阅（「启动 claude」按钮置灰加速）。
#[derive(Clone, Serialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct ClaudeRuntimeChangedPayload {
    /// pane 锚点（issueId::paneId，store key）。
    pub pane: String,
    /// transcript JSONL 绝对路径（SessionStart 绑定）。
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
        // 含已删除的历史字段（tool_name/message 等）与未知字段：宽容透传不炸。
        let json = r#"{
            "hook_event_name": "SessionStart",
            "session_id": "s1",
            "tool_name": "Bash",
            "message": "approve",
            "delta": "x",
            "unknown_field": 123
        }"#;
        let p: HookPayload = serde_json::from_str(json).unwrap();
        assert_eq!(p.hook_event_name, "SessionStart");
        assert_eq!(p.session_id.as_deref(), Some("s1"));
        assert!(p.transcript_path.is_none());
        assert!(p.launch_token.is_none());
    }

    #[test]
    fn hook_payload_missing_all_optionals() {
        let p: HookPayload = serde_json::from_str(r#"{"hook_event_name":"Stop"}"#).unwrap();
        assert_eq!(p.hook_event_name, "Stop");
        assert!(p.session_id.is_none());
        assert!(p.transcript_path.is_none());
        assert!(p.launch_token.is_none());
        assert!(p.agent_id.is_none());
    }

    /// 脚本注入后的载荷形态：launch_token 为首字段 + claude 原生字段共存。
    #[test]
    fn hook_payload_parses_injected_launch_token() {
        let json =
            r#"{"launch_token":"tok-abc","hook_event_name":"SessionStart","session_id":"s1"}"#;
        let p: HookPayload = serde_json::from_str(json).unwrap();
        assert_eq!(p.hook_event_name, "SessionStart");
        assert_eq!(p.launch_token.as_deref(), Some("tok-abc"));
        assert_eq!(p.session_id.as_deref(), Some("s1"));
    }

    /// agent_id 类型宽容：字符串与对象形态都不得导致整行反序列化失败。
    #[test]
    fn hook_payload_tolerates_variant_field_types() {
        let p: HookPayload =
            serde_json::from_str(r#"{"hook_event_name":"SessionStart","agent_id":{"nested":1}}"#)
                .unwrap();
        assert!(p.agent_id.is_some());
    }

    #[test]
    fn hook_payload_missing_event_name_defaults_empty() {
        let p: HookPayload = serde_json::from_str(r#"{"session_id":"s1"}"#).unwrap();
        assert_eq!(p.hook_event_name, "");
    }
}
