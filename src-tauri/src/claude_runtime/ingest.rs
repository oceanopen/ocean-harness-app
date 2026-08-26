// 载荷归一化状态机（T1.3）：spool 行 → HookPayload → 状态迁移决策 → store 更新
// + 快照落盘 + 事件 emit。
//
// 设计对齐 orca server.ts getAgentStatusDisposition / normalizeClaudeEvent
// （v1.4.178 重新梳理结论）：
//   - launch_token 围栏：store token 在场且与载荷不符 → 丢弃（防同 pane 重启
//     claude 后旧会话迟到事件覆盖新状态，orca #1146）；载荷无 token → 放行
//     （T1.4 env 注入落地前的兼容窗口）。
//   - 子代理防御：带 agent_id 的 SessionStart 忽略（Task 子进程不得翻转 pane 状态）。
//   - Notification 不入状态机（orca 已移除：claude idle 时也发 Notification，
//     误置 waiting）；注册保留（T1.2），仅作观察渠道。
//   - MessageDisplay 推 working 态 + delta/index 拼接 preview_text（T3.1 流式
//     气泡数据源，T1.3 砍掉的拼接在此复活）。带 agent_id 的 MessageDisplay
//     丢弃（子代理防御，同 SessionStart）。
//   - 任意带 session_id 的事件可兜底绑定（对齐 orca providerSession——不单靠
//     SessionStart，其丢失时不至于全盲）。
//
// 纯函数 decide 与效果层 ingest 分离：decide 收值参数可单测（无 AppHandle）。

use serde_json::Value;
use tauri::{AppHandle, Emitter, Manager};

use super::store::{self, ClaudeRuntimeState, ClaudeRuntimeStore};
use super::types::{ClaudeNotification, ClaudeRuntimeStatus, HookPayload};
use crate::shared::events::EVENT_CLAUDE_RUNTIME_CHANGED;

/// 归一化决策：丢弃 / 应用（附带副作用标记）。
#[derive(Debug, PartialEq)]
pub enum Decision {
    /// 围栏不匹配 / 子代理 / 忽略的事件——不更新 store 不 emit。
    Drop,
    /// 应用新状态。
    /// - truncate_spool：批内出现已接受的 SessionStart，watch 层批后截断该
    ///   pane 的 spool 文件（新会话新起点）+ offset 归零。
    /// - persist：落快照（仅 SessionStart 绑定变更时——hydrate 会重置其余
    ///   全部字段，快照唯一有效信息就是 session 绑定，避免流式期写盘风暴）。
    Apply {
        state: ClaudeRuntimeState,
        truncate_spool: bool,
        persist: bool,
    },
}

/// 单行归一化决策（纯函数）。current 为 None 表示 pane 首个事件（从默认态起步）。
pub fn decide(current: Option<&ClaudeRuntimeState>, payload: &HookPayload, now_ms: i64) -> Decision {
    let base = current.cloned().unwrap_or_default();

    // launch_token 围栏：store 已绑定代际标且载荷携带不同 token → 僵尸事件丢弃。
    // 例外（对齐 orca server.ts:1129）：tokened SessionStart 直接放行并重绑——
    // 同 pane 重启新 claude 的首个事件就是它，若也被围栏挡住则永远无法换代。
    // 载荷无 token（T1.4 前窗口 / 脚本未升级）放行。
    if let (Some(bound), Some(incoming)) = (base.launch_token.as_deref(), payload.launch_token.as_deref()) {
        let is_session_start = payload.hook_event_name == "SessionStart";
        if bound != incoming && !is_session_start {
            return Decision::Drop;
        }
    }

    // 会话绑定兜底（providerSession 思路）：任意带 session_id 的事件都能补绑
    // session_id/transcript_path（SessionStart 丢失时不至于全盲）。
    let mut state = base;
    if state.claude_session_id.is_none() {
        if let Some(sid) = payload.session_id.as_deref() {
            if !sid.is_empty() {
                state.claude_session_id = Some(sid.to_string());
            }
        }
    }
    if state.transcript_path.is_none() {
        if let Some(tp) = payload.transcript_path.as_deref() {
            if !tp.is_empty() {
                state.transcript_path = Some(tp.to_string());
            }
        }
    }

    state.updated_at = now_ms;

    match payload.hook_event_name.as_str() {
        // 子代理的 SessionStart（带 agent_id）忽略——Task 子进程不得翻转 pane 状态。
        "SessionStart" if payload.agent_id.is_some() => Decision::Drop,
        "SessionStart" => {
            state.launch_token = payload.launch_token.clone();
            if let Some(sid) = payload.session_id.as_deref() {
                state.claude_session_id = Some(sid.to_string());
            }
            if let Some(tp) = payload.transcript_path.as_deref() {
                state.transcript_path = Some(tp.to_string());
            }
            state.status = ClaudeRuntimeStatus::Idle;
            state.preview_text = None;
            state.preview_index = None;
            state.notification = None;
            Decision::Apply {
                state,
                truncate_spool: true,
                persist: true,
            }
        }
        "UserPromptSubmit" => {
            state.status = ClaudeRuntimeStatus::Working;
            // 新回合起步：上一回合残留 preview 作废（否则 working 态下
            // deriveStreamingText 会拿旧文本当流式预览显示）。
            state.preview_text = None;
            state.preview_index = None;
            state.notification = None;
            Decision::Apply {
                state,
                truncate_spool: false,
                persist: false,
            }
        }
        // 子代理的 MessageDisplay 丢弃——Task 子进程的流式文本不得混入
        // 主 pane 预览（防御口径同 SessionStart）。
        "MessageDisplay" if payload.agent_id.is_some() => Decision::Drop,
        "MessageDisplay" => {
            state.status = ClaudeRuntimeStatus::Working;
            apply_preview_delta(&mut state, payload);
            state.notification = None;
            Decision::Apply {
                state,
                truncate_spool: false,
                persist: false,
            }
        }
        "Stop" | "StopFailure" => {
            state.status = ClaudeRuntimeStatus::Idle;
            state.preview_text = None;
            state.notification = None;
            Decision::Apply {
                state,
                truncate_spool: false,
                persist: false,
            }
        }
        "PermissionRequest" => {
            state.status = ClaudeRuntimeStatus::Waiting;
            state.notification = Some(notification_from(payload));
            Decision::Apply {
                state,
                truncate_spool: false,
                persist: false,
            }
        }
        // Notification 不入状态机（orca v1.4.178 已移除：claude idle 时也发
        // "waiting for your input"，误置 waiting）。未知事件静默忽略。
        _ => Decision::Drop,
    }
}

/// MessageDisplay delta/index → preview 拼接（T3.1 流式气泡数据源）。
///
/// 游标规则（T1.3 原稿设计「同一消息按 index 追加」）：
///   - index=0：新消息起点，preview 重置为该 delta；
///   - index=游标+1：顺序追加；
///   - index<=游标：重复/迟到事件，delta 丢弃（防 spool 重读双份文本）；
///   - 跳号：当新流重置（宁重置不粘错位文本）；
///   - 无 index：宽容追加（旧形态/防御性）。
/// final 标记不参与：定稿文本保持展示，Stop 清空。
fn apply_preview_delta(state: &mut ClaudeRuntimeState, payload: &HookPayload) {
    let Some(delta) = payload.delta.as_deref() else {
        return; // 无 delta 的 MessageDisplay：仅状态推进
    };
    let index = payload.index.as_ref().and_then(Value::as_i64);
    match index {
        Some(0) => {
            state.preview_text = Some(delta.to_string());
            state.preview_index = Some(0);
        }
        Some(i) => {
            let cursor = state.preview_index;
            if cursor.is_some_and(|last| last >= i) {
                return; // 重复/迟到
            }
            if cursor.is_some_and(|last| last + 1 == i) {
                append_preview_text(state, delta);
                state.preview_index = Some(i);
            } else {
                // 跳号（或首见非 0 序号）：当新流重置。
                state.preview_text = Some(delta.to_string());
                state.preview_index = Some(i);
            }
        }
        None => append_preview_text(state, delta),
    }
}

/// preview 追加 delta（原地 push，无整串 clone）。
fn append_preview_text(state: &mut ClaudeRuntimeState, delta: &str) {
    if let Some(text) = state.preview_text.as_mut() {
        text.push_str(delta);
    } else {
        state.preview_text = Some(delta.to_string());
    }
}

/// PermissionRequest 载荷 → ClaudeNotification。message 取 payload.message
/// （缺省用 tool_name 合成）；tool_input 序列化为字符串（前端卡片渲染用）；
/// permission_suggestions best-effort 提取（字符串数组形态，其他形态丢弃）。
fn notification_from(payload: &HookPayload) -> ClaudeNotification {
    let message = payload
        .message
        .clone()
        .or_else(|| payload.tool_name.clone().map(|t| format!("approve {t}")))
        .unwrap_or_default();
    ClaudeNotification {
        message,
        tool_name: payload.tool_name.clone(),
        tool_input: payload
            .tool_input
            .as_ref()
            .map(|v| v.to_string()),
        permission_suggestions: permission_suggestions(payload),
    }
}

/// permission_suggestions 宽容提取：claude 载荷为字符串数组；任何其他形态
/// （对象/嵌套）返回 None（T4.1 渲染时按 None 处理，不 panic）。
fn permission_suggestions(payload: &HookPayload) -> Option<Vec<String>> {
    let Value::Array(items) = payload.tool_input.as_ref()?.get("permission_suggestions")? else {
        return None;
    };
    let list = items
        .iter()
        .filter_map(|v| v.as_str().map(String::from))
        .collect::<Vec<_>>();
    (!list.is_empty()).then_some(list)
}

/// 效果层：单行 ingest——反序列化 → decide → store 更新 → persist（按需）→ emit。
/// 损坏行 skip + warn（不 panic）；返回是否要求截断 spool（watch 层批后执行）。
pub fn ingest(app: &AppHandle, pane: &str, line: &str) -> bool {
    let payload: HookPayload = match serde_json::from_str(line) {
        Ok(p) => p,
        Err(e) => {
            log::warn!("[claude_runtime] spool line parse failed, skipping: {}", e);
            return false;
        }
    };
    let now_ms = now_millis();

    let decision = {
        let store = app.state::<ClaudeRuntimeStore>();
        let mut map = store
            .0
            .lock()
            .expect("ClaudeRuntimeStore mutex poisoned");
        match decide(map.get(pane), &payload, now_ms) {
            Decision::Drop => Decision::Drop,
            d @ Decision::Apply { .. } => {
                if let Decision::Apply { ref state, .. } = d {
                    map.insert(pane.to_string(), state.clone());
                }
                d
            }
        }
    };

    let Decision::Apply {
        state,
        truncate_spool,
        persist,
    } = decision
    else {
        return false;
    };

    if persist {
        let store = app.state::<ClaudeRuntimeStore>();
        store::persist(&store);
    }
    if let Err(e) = app.emit(EVENT_CLAUDE_RUNTIME_CHANGED, &state.to_payload(pane)) {
        log::warn!("[claude_runtime] emit claude-runtime:changed failed: {}", e);
    }
    truncate_spool
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
            status: ClaudeRuntimeStatus::Idle,
            preview_text: None,
            preview_index: None,
            notification: None,
            updated_at: 1,
        }
    }

    #[test]
    fn session_start_binds_and_truncates() {
        let p: HookPayload = serde_json::from_str(
            r#"{"hook_event_name":"SessionStart","session_id":"s2","transcript_path":"/tmp/t2.jsonl","launch_token":"tokB"}"#,
        )
        .unwrap();
        let d = decide(Some(&state_with_token("tokA")), &p, 100);
        let Decision::Apply {
            state,
            truncate_spool,
            persist,
        } = d
        else {
            panic!("expected Apply");
        };
        assert!(truncate_spool, "SessionStart must request spool truncation");
        assert!(persist);
        assert_eq!(state.claude_session_id.as_deref(), Some("s2"));
        assert_eq!(state.transcript_path.as_deref(), Some("/tmp/t2.jsonl"));
        assert_eq!(state.launch_token.as_deref(), Some("tokB"));
        assert_eq!(state.status, ClaudeRuntimeStatus::Idle);
    }

    #[test]
    fn token_fence_drops_stale_events() {
        let current = state_with_token("tokB");
        let mut p = payload("Stop");
        p.launch_token = Some("tokA".into()); // 旧代际（同 pane 重启前的 claude）
        assert_eq!(decide(Some(&current), &p, 100), Decision::Drop);
    }

    /// 僵尸时序（orca #1146）：旧 claude 的 Stop 迟到 → 丢弃；新 claude 的
    /// UserPromptSubmit（token 匹配）正常生效。
    #[test]
    fn zombie_stop_dropped_new_prompt_accepted() {
        let current = state_with_token("tokB");
        let mut stale = payload("Stop");
        stale.launch_token = Some("tokA".into());
        assert_eq!(decide(Some(&current), &stale, 100), Decision::Drop);

        let mut fresh = payload("UserPromptSubmit");
        fresh.launch_token = Some("tokB".into());
        let Decision::Apply { state, .. } = decide(Some(&current), &fresh, 100) else {
            panic!("expected Apply");
        };
        assert_eq!(state.status, ClaudeRuntimeStatus::Working);
    }

    /// 载荷无 token（T1.4 前窗口）放行——围栏只在双方都有 token 时比对。
    #[test]
    fn missing_token_passes_fence() {
        let current = state_with_token("tokB");
        let d = decide(Some(&current), &payload("UserPromptSubmit"), 100);
        assert!(matches!(d, Decision::Apply { .. }));
    }

    #[test]
    fn subagent_session_start_dropped() {
        let mut p = payload("SessionStart");
        p.agent_id = Some(Value::String("agent-1".into()));
        p.launch_token = Some("tokB".into());
        assert_eq!(decide(Some(&state_with_token("tokB")), &p, 100), Decision::Drop);
    }

    #[test]
    fn state_machine_transitions() {
        // working → idle（Stop / StopFailure 均为回合边界，清 preview/notification）
        let mut working = state_with_token("tokB");
        working.status = ClaudeRuntimeStatus::Working;
        working.preview_text = Some("thinking".into());
        for event in ["Stop", "StopFailure"] {
            let Decision::Apply { state, .. } = decide(Some(&working), &payload(event), 100)
            else {
                panic!("{event} must apply");
            };
            assert_eq!(state.status, ClaudeRuntimeStatus::Idle);
            assert_eq!(state.preview_text, None);
            assert_eq!(state.notification, None);
        }

        // UserPromptSubmit / MessageDisplay → working + 清 notification
        let mut waiting = state_with_token("tokB");
        waiting.status = ClaudeRuntimeStatus::Waiting;
        waiting.notification = Some(ClaudeNotification {
            message: "approve".into(),
            tool_name: None,
            tool_input: None,
            permission_suggestions: None,
        });
        for event in ["UserPromptSubmit", "MessageDisplay"] {
            let Decision::Apply { state, .. } = decide(Some(&waiting), &payload(event), 100)
            else {
                panic!("{event} must apply");
            };
            assert_eq!(state.status, ClaudeRuntimeStatus::Working);
            assert_eq!(state.notification, None);
        }
    }

    /// Notification 不入状态机（orca v1.4.178：claude idle 时也发，误置 waiting）。
    #[test]
    fn notification_is_ignored() {
        let mut p = payload("Notification");
        p.message = Some("Claude is waiting for your input".into());
        assert_eq!(decide(Some(&state_with_token("tokB")), &p, 100), Decision::Drop);
        assert_eq!(decide(None, &p, 100), Decision::Drop);
    }

    /// 未知事件静默忽略（未来事件名漂移不炸状态机）。
    #[test]
    fn unknown_event_dropped() {
        assert_eq!(decide(None, &payload("SomeFutureEvent"), 100), Decision::Drop);
    }

    /// 任意带 session_id 的事件可兜底绑定（SessionStart 丢失场景）。
    #[test]
    fn any_event_backfills_session_binding() {
        let mut p = payload("UserPromptSubmit");
        p.session_id = Some("s9".into());
        p.transcript_path = Some("/tmp/t9.jsonl".into());
        let Decision::Apply { state, .. } = decide(None, &p, 100) else {
            panic!("expected Apply");
        };
        assert_eq!(state.claude_session_id.as_deref(), Some("s9"));
        assert_eq!(state.transcript_path.as_deref(), Some("/tmp/t9.jsonl"));
    }

    #[test]
    fn permission_request_builds_notification() {
        let mut p = payload("PermissionRequest");
        p.message = Some("Claude needs approval".into());
        p.tool_name = Some("Bash".into());
        p.tool_input = Some(serde_json::from_str(
            r#"{"command":"ls","permission_suggestions":["Allow once","Always allow"]}"#,
        )
        .unwrap());
        let Decision::Apply { state, .. } = decide(None, &p, 100) else {
            panic!("expected Apply");
        };
        assert_eq!(state.status, ClaudeRuntimeStatus::Waiting);
        let n = state.notification.unwrap();
        assert_eq!(n.message, "Claude needs approval");
        assert_eq!(n.tool_name.as_deref(), Some("Bash"));
        assert!(n.tool_input.as_deref().unwrap().contains("ls"));
        assert_eq!(
            n.permission_suggestions,
            Some(vec!["Allow once".into(), "Always allow".into()])
        );
    }

    /// permission_suggestions 非数组/非字符串元素形态 → None（不 panic）。
    #[test]
    fn permission_suggestions_tolerates_odd_shapes() {
        let mut p = payload("PermissionRequest");
        p.message = Some("approve".into());
        p.tool_input = Some(serde_json::from_str(r#"{"permission_suggestions":{"a":1}}"#).unwrap());
        let Decision::Apply { state, .. } = decide(None, &p, 100) else {
            panic!("expected Apply");
        };
        assert_eq!(state.notification.unwrap().permission_suggestions, None);
    }

    /// message 缺省时用 tool_name 合成通知文案。
    #[test]
    fn notification_message_falls_back_to_tool_name() {
        let mut p = payload("PermissionRequest");
        p.tool_name = Some("Bash".into());
        let Decision::Apply { state, .. } = decide(None, &p, 100) else {
            panic!("expected Apply");
        };
        assert_eq!(state.notification.unwrap().message, "approve Bash");
    }

    // ===== MessageDisplay delta 拼接（T3.1 流式气泡数据源）=====

    fn message_display(delta: &str, index: i64, final_flag: bool) -> HookPayload {
        serde_json::from_value(serde_json::json!({
            "hook_event_name": "MessageDisplay",
            "delta": delta,
            "index": index,
            "final": final_flag,
        }))
        .unwrap()
    }

    /// 顺序追加：index 0 起步 + index 1 追加 + final 保持定稿文本。
    #[test]
    fn message_display_appends_preview_in_order() {
        let current = state_with_token("tokB");
        let Decision::Apply { state: s1, .. } =
            decide(Some(&current), &message_display("Hel", 0, false), 100)
        else {
            panic!("expected Apply");
        };
        assert_eq!(s1.preview_text.as_deref(), Some("Hel"));
        assert_eq!(s1.status, ClaudeRuntimeStatus::Working);

        let Decision::Apply { state: s2, .. } =
            decide(Some(&s1), &message_display("lo 世界", 1, true), 101)
        else {
            panic!("expected Apply");
        };
        assert_eq!(s2.preview_text.as_deref(), Some("Hello 世界"), "final 保持定稿文本");
    }

    /// index 0 重置：新消息起点，旧 preview 作废。
    #[test]
    fn message_display_index_zero_resets_preview() {
        let mut current = state_with_token("tokB");
        current.preview_text = Some("旧消息".into());
        current.preview_index = Some(5);
        let Decision::Apply { state, .. } =
            decide(Some(&current), &message_display("New", 0, false), 100)
        else {
            panic!("expected Apply");
        };
        assert_eq!(state.preview_text.as_deref(), Some("New"));
    }

    /// 重复/迟到 index：delta 丢弃（spool 重读/事件重发不双份文本）。
    #[test]
    fn message_display_duplicate_index_dropped() {
        let mut current = state_with_token("tokB");
        current.preview_text = Some("Hello".into());
        current.preview_index = Some(1);
        let Decision::Apply { state, .. } =
            decide(Some(&current), &message_display("Hello", 1, false), 100)
        else {
            panic!("expected Apply");
        };
        assert_eq!(state.preview_text.as_deref(), Some("Hello"), "重复 index 不追加");
    }

    /// 子代理 MessageDisplay 丢弃（防御口径同 SessionStart）。
    #[test]
    fn subagent_message_display_dropped() {
        let mut p = message_display("sub text", 0, false);
        p.agent_id = Some(Value::String("agent-1".into()));
        assert_eq!(decide(Some(&state_with_token("tokB")), &p, 100), Decision::Drop);
    }

    /// UserPromptSubmit 清残留 preview：新回合起步旧流式文本作废。
    #[test]
    fn user_prompt_clears_stale_preview() {
        let mut current = state_with_token("tokB");
        current.status = ClaudeRuntimeStatus::Idle;
        current.preview_text = Some("上回合残文".into());
        current.preview_index = Some(2);
        let Decision::Apply { state, .. } = decide(Some(&current), &payload("UserPromptSubmit"), 100)
        else {
            panic!("expected Apply");
        };
        assert_eq!(state.status, ClaudeRuntimeStatus::Working);
        assert_eq!(state.preview_text, None);
        assert_eq!(state.preview_index, None);
    }
}
