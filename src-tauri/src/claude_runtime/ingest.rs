// 载荷归一化（T1.3 → chat 退役裁剪）：spool 行 → HookPayload → 决策 → store 更新
// + 快照落盘 + 事件 emit。
//
// chat 模式退役（2026-08）后状态机/预览/通知臂全删，仅剩会话绑定链：
//   - launch_token 围栏：store token 在场且与载荷不符 → 丢弃（防同 pane 重启
//     claude 后旧会话迟到事件覆盖绑定，orca #1146）；tokened SessionStart 例外
//     放行并重绑（同 pane 重启新 claude 的首个事件，挡住则永远无法换代）；
//     载荷无 token（T1.4 前窗口 / 脚本未升级）放行。
//   - 子代理防御：带 agent_id 的 SessionStart 忽略（Task 子进程不得抢占 pane 绑定）。
//   - SessionStart：绑定 session_id/transcript_path/launch_token + 截断 spool
//     （新会话新起点）+ persist（快照唯一有效信息就是 session 绑定）。
//   - 其余事件（旧工作区残留注册的退休事件噪声 / 未来事件名漂移）一律 Drop。
//
// 纯函数 decide 与效果层 ingest 分离：decide 收值参数可单测（无 AppHandle）。

use tauri::{AppHandle, Emitter, Manager};

use super::store::{self, ClaudeRuntimeState, ClaudeRuntimeStore};
use super::types::HookPayload;
use crate::shared::events::EVENT_CLAUDE_RUNTIME_CHANGED;

/// 归一化决策：丢弃 / 应用。chat 退役后唯一 Apply 源是 SessionStart——
/// 副作用恒定（ingest 层：persist 落快照 + 返回 true 请 watch 截断 spool），
/// 多臂状态机时代的标志位已收敛。
#[derive(Debug, PartialEq)]
pub enum Decision {
    /// 围栏不匹配 / 子代理 / 非 SessionStart 事件——不更新 store 不 emit。
    Drop,
    /// 应用新状态（SessionStart 绑定）。
    Apply { state: ClaudeRuntimeState },
}

/// 单行归一化决策（纯函数）。current 为 None 表示 pane 首个事件（从默认态起步）。
pub fn decide(
    current: Option<&ClaudeRuntimeState>,
    payload: &HookPayload,
    now_ms: i64,
) -> Decision {
    let mut state = current.cloned().unwrap_or_default();

    // launch_token 围栏：store 已绑定代际标且载荷携带不同 token → 僵尸事件丢弃。
    // 例外（对齐 orca server.ts:1129）：tokened SessionStart 直接放行并重绑——
    // 同 pane 重启新 claude 的首个事件就是它，若也被围栏挡住则永远无法换代。
    // 载荷无 token（T1.4 前窗口 / 脚本未升级）放行。chat 退役后仅 SessionStart
    // 进 Apply（其余事件本就落 Drop），围栏作为换代语义的显式防线保留。
    if let (Some(bound), Some(incoming)) = (
        state.launch_token.as_deref(),
        payload.launch_token.as_deref(),
    ) {
        let is_session_start = payload.hook_event_name == "SessionStart";
        if bound != incoming && !is_session_start {
            return Decision::Drop;
        }
    }

    state.updated_at = now_ms;

    match payload.hook_event_name.as_str() {
        // 子代理的 SessionStart（带 agent_id）忽略——Task 子进程不得抢占 pane 绑定。
        "SessionStart" if payload.agent_id.is_some() => Decision::Drop,
        "SessionStart" => {
            state.launch_token = payload.launch_token.clone();
            if let Some(sid) = non_empty(payload.session_id.as_deref()) {
                state.claude_session_id = Some(sid.to_string());
            }
            if let Some(tp) = non_empty(payload.transcript_path.as_deref()) {
                state.transcript_path = Some(tp.to_string());
            }
            Decision::Apply { state }
        }
        // 其余事件 Drop：chat 退役后无消费方（旧工作区残留注册的退休事件
        // 仍会写 spool，在此统一消化；未来事件名漂移同理）。
        _ => Decision::Drop,
    }
}

/// 非空字符串判定（绑定字段不写空值）。
fn non_empty(s: Option<&str>) -> Option<&str> {
    match s {
        Some(v) if !v.is_empty() => Some(v),
        _ => None,
    }
}

/// 效果层：单行 ingest——反序列化 → decide → store 更新 → persist → emit。
/// 损坏行 skip + warn（不 panic）；Apply（SessionStart）返回 true 请求截断
/// spool（watch 层批后执行），Drop 返回 false。
pub fn ingest(app: &AppHandle, pane: &str, line: &str) -> bool {
    let payload: HookPayload = match serde_json::from_str(line) {
        Ok(p) => p,
        Err(e) => {
            log::warn!(
                "[claude_runtime] spool line parse failed, skipping: {}",
                e
            );
            return false;
        }
    };
    let now_ms = now_millis();

    let apply_state = {
        let store = app.state::<ClaudeRuntimeStore>();
        let mut map = store
            .0
            .lock()
            .expect("ClaudeRuntimeStore mutex poisoned");
        match decide(map.get(pane), &payload, now_ms) {
            Decision::Drop => None,
            Decision::Apply { state } => {
                map.insert(pane.to_string(), state.clone());
                Some(state)
            }
        }
    };

    let Some(state) = apply_state else {
        return false;
    };
    // Apply 恒为 SessionStart 绑定：落快照（唯一有效信息）。
    {
        let store = app.state::<ClaudeRuntimeStore>();
        store::persist(&store);
    }
    if let Err(e) = app.emit(
        EVENT_CLAUDE_RUNTIME_CHANGED,
        &state.to_payload(pane),
    ) {
        log::warn!(
            "[claude_runtime] emit claude-runtime:changed failed: {}",
            e
        );
    }
    // Apply 恒为 SessionStart：请求 watch 批后截断该 pane 的 spool（新会话新起点）。
    true
}

/// 毫秒时间戳（ingest 效果层用；decide 收参数以便单测恒定）。
fn now_millis() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn payload(event: &str) -> HookPayload {
        serde_json::from_str(&format!(r#"{{"hook_event_name":"{event}"}}"#)).unwrap()
    }

    fn state_with_token(token: &str) -> ClaudeRuntimeState {
        ClaudeRuntimeState {
            launch_token: Some(token.into()),
            claude_session_id: Some("sess1".into()),
            transcript_path: Some("/tmp/t.jsonl".into()),
            updated_at: 1,
        }
    }

    /// SessionStart 绑定 + 换代：跨代 token（tokA→tokB）经围栏例外放行并重绑，
    /// 请求截断 spool 与 persist。
    #[test]
    fn session_start_binds_and_truncates() {
        let p: HookPayload = serde_json::from_str(
            r#"{"hook_event_name":"SessionStart","session_id":"s2","transcript_path":"/tmp/t2.jsonl","launch_token":"tokB"}"#,
        )
        .unwrap();
        let d = decide(Some(&state_with_token("tokA")), &p, 100);
        let Decision::Apply { state } = d else {
            panic!("expected Apply");
        };
        assert_eq!(state.claude_session_id.as_deref(), Some("s2"));
        assert_eq!(
            state.transcript_path.as_deref(),
            Some("/tmp/t2.jsonl")
        );
        assert_eq!(state.launch_token.as_deref(), Some("tokB"));
    }

    /// 围栏：非 SessionStart 事件带旧代 token → 丢弃（chat 退役后这类事件
    /// 在 match 也落 Drop，围栏是更显式的第一道防线）。
    #[test]
    fn token_fence_drops_stale_events() {
        let current = state_with_token("tokB");
        let mut p = payload("Stop");
        p.launch_token = Some("tokA".into()); // 旧代际（同 pane 重启前的 claude）
        assert_eq!(decide(Some(&current), &p, 100), Decision::Drop);
    }

    #[test]
    fn subagent_session_start_dropped() {
        let mut p = payload("SessionStart");
        p.agent_id = Some(serde_json::Value::String("agent-1".into()));
        p.launch_token = Some("tokB".into());
        assert_eq!(
            decide(Some(&state_with_token("tokB")), &p, 100),
            Decision::Drop
        );
    }

    /// 非 SessionStart 事件（含退休事件噪声与未知事件）一律 Drop，不更新绑定。
    #[test]
    fn non_session_start_events_dropped() {
        for event in [
            "UserPromptSubmit",
            "MessageDisplay",
            "Stop",
            "StopFailure",
            "PreToolUse",
            "PermissionRequest",
            "Notification",
            "SomeFutureEvent",
        ] {
            assert_eq!(
                decide(
                    Some(&state_with_token("tokB")),
                    &payload(event),
                    100
                ),
                Decision::Drop,
                "{event} must drop"
            );
        }
        // 无 current（pane 首事件）同理。
        assert_eq!(
            decide(None, &payload("Stop"), 100),
            Decision::Drop
        );
    }

    /// SessionStart 空字段不覆盖既有绑定（绑定字段不写空值）。
    #[test]
    fn session_start_empty_fields_keep_binding() {
        let p: HookPayload =
            serde_json::from_str(r#"{"hook_event_name":"SessionStart","launch_token":"tokB"}"#)
                .unwrap();
        let Decision::Apply { state } = decide(Some(&state_with_token("tokA")), &p, 100) else {
            panic!("expected Apply");
        };
        assert_eq!(state.claude_session_id.as_deref(), Some("sess1"));
        assert_eq!(
            state.transcript_path.as_deref(),
            Some("/tmp/t.jsonl")
        );
    }
}
