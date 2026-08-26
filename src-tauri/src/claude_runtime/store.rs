// ClaudeRuntimeStore：pane → ClaudeRuntimeState 的运行时状态存储（hook 事件唯一状态源）。
//
// 范式对齐 shared/state/transcript.rs（TranscriptWatchStore）与 claude_sessions.rs：
// `pub struct X(pub Mutex<HashMap<..>>)` + `#[derive(Default)]` + init 时 app.manage。
// 锁 poison 走 expect panic 兜底（短临界区，poison 只在持锁线程 panic 时发生）。
//
// 快照持久化：store 变更时 persist 覆写 app_data_dir/claude_runtime_snapshot.json，
// 启动 init hydrate——支撑 app 重启后 resume（T5.2）与冷启动展示。

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::{Mutex, OnceLock};

use serde::{Deserialize, Serialize};
use tauri::{App, Manager};

use super::types::{ClaudeNotification, ClaudeRuntimeChangedPayload, ClaudeRuntimeStatus};

/// 单 pane 的运行时状态（store value，内部类型，不 specta 导出）。
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ClaudeRuntimeState {
    /// 本次 spawn 的代际标（env WE_TERM_LAUNCH_TOKEN，T1.4 注入）；SessionStart 绑定，
    /// 陈旧事件围栏用（T1.3）。
    pub launch_token: Option<String>,
    /// claude 会话 ID（resume 用）。
    pub claude_session_id: Option<String>,
    /// transcript JSONL 绝对路径。
    pub transcript_path: Option<String>,
    /// 运行时状态。
    pub status: ClaudeRuntimeStatus,
    /// 生成中的预览文本。
    pub preview_text: Option<String>,
    /// preview 拼接游标（最近一次 MessageDisplay 的 index；T3.1）。瞬态字段：
    /// persist 仅 SessionStart 触发（彼时已清空），快照实质不携带；hydrate
    /// 重置。serde default 兼容旧快照缺字段。
    #[serde(default)]
    pub preview_index: Option<i64>,
    /// 审批/提问通知（waiting 态）。
    pub notification: Option<ClaudeNotification>,
    /// 最后更新时间（毫秒时间戳）。
    pub updated_at: i64,
}

impl ClaudeRuntimeState {
    /// 映射为前端事件 payload（加 pane，去内部 launch_token）。T1.3 emit / T2.1 命令复用。
    pub fn to_payload(&self, pane: &str) -> ClaudeRuntimeChangedPayload {
        ClaudeRuntimeChangedPayload {
            pane: pane.to_string(),
            status: self.status,
            preview_text: self.preview_text.clone(),
            notification: self.notification.clone(),
            transcript_path: self.transcript_path.clone(),
            claude_session_id: self.claude_session_id.clone(),
            last_updated_at: self.updated_at,
        }
    }
}

/// 运行时状态存储。key 为 pane 锚点（issueId::paneId）。
#[derive(Default)]
pub struct ClaudeRuntimeStore(pub Mutex<HashMap<String, ClaudeRuntimeState>>);

/// 快照文件路径（app_data_dir/claude_runtime_snapshot.json）。init 注入，
/// 测试可经 persist_to/hydrate_from 直接传路径绕过。
static SNAPSHOT_PATH: OnceLock<PathBuf> = OnceLock::new();

/// 初始化：注入快照路径 → hydrate → app.manage。须早于 pty spawn 能力（lib.rs setup）。
pub fn init(app: &mut App) -> Result<(), Box<dyn std::error::Error>> {
    let data_dir = app.path().app_data_dir()?;
    let _ = SNAPSHOT_PATH.set(data_dir.join("claude_runtime_snapshot.json"));
    let store = ClaudeRuntimeStore::default();
    if let Some(path) = SNAPSHOT_PATH.get() {
        hydrate_from(&store, path);
    }
    app.manage(store);
    Ok(())
}

/// store 变更后落盘（T1.3 ingest 更新后调用）。序列化在锁内、IO 在锁外，
/// 避免写盘阻塞其它读写方。
pub fn persist(store: &ClaudeRuntimeStore) {
    let Some(path) = SNAPSHOT_PATH.get() else {
        return;
    };
    persist_to(store, path);
}

/// 读快照填充 store（启动 hydrate）。文件缺失/损坏静默跳过（不 panic，空态启动）。
///
/// 陈旧态重置：app 重启后 claude 实际状态未知，每条恢复的 state 重置
/// status=idle + 清空 launch_token/preview_text/preview_index/notification——
/// 避免快照陈旧 working/waiting 态误导前端门槛与 T6.1 fallback 判定；
/// claude_session_id/transcript_path 保留（resume 与定位用），等新
/// SessionStart 重新绑定 token。
fn hydrate_from(store: &ClaudeRuntimeStore, path: &Path) {
    let Ok(content) = std::fs::read_to_string(path) else {
        return;
    };
    let map = match serde_json::from_str::<HashMap<String, ClaudeRuntimeState>>(&content) {
        Ok(m) => m,
        Err(e) => {
            log::warn!(
                "[claude_runtime] snapshot parse failed, ignoring: {}",
                e
            );
            return;
        }
    };
    let mut guard = store
        .0
        .lock()
        .expect("ClaudeRuntimeStore mutex poisoned");
    *guard = map
        .into_iter()
        .map(|(pane, mut s)| {
            s.status = ClaudeRuntimeStatus::Idle;
            s.launch_token = None;
            s.preview_text = None;
            s.preview_index = None;
            s.notification = None;
            (pane, s)
        })
        .collect();
}

/// 序列化全量 map → temp+rename 原子写。序列化失败仅 warn（不阻塞主流程）。
fn persist_to(store: &ClaudeRuntimeStore, path: &Path) {
    let json = {
        let map = store
            .0
            .lock()
            .expect("ClaudeRuntimeStore mutex poisoned");
        match serde_json::to_string_pretty(&*map) {
            Ok(j) => j,
            Err(e) => {
                log::warn!(
                    "[claude_runtime] snapshot serialize failed: {}",
                    e
                );
                return;
            }
        }
    };
    let tmp = path.with_extension("json.tmp");
    if let Err(e) = std::fs::write(&tmp, &json) {
        log::warn!("[claude_runtime] snapshot write failed: {}", e);
        return;
    }
    if let Err(e) = std::fs::rename(&tmp, path) {
        log::warn!("[claude_runtime] snapshot rename failed: {}", e);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_state() -> ClaudeRuntimeState {
        ClaudeRuntimeState {
            launch_token: Some("tok1".into()),
            claude_session_id: Some("sess1".into()),
            transcript_path: Some("/tmp/t.jsonl".into()),
            status: ClaudeRuntimeStatus::Working,
            preview_text: Some("thinking...".into()),
            preview_index: Some(3),
            notification: Some(ClaudeNotification {
                message: "approve Bash".into(),
                tool_name: Some("Bash".into()),
                tool_input: None,
                permission_suggestions: None,
            }),
            updated_at: 123_456,
        }
    }

    /// 落盘→读回链路：快照保留全部字段（roundtrip 完整性），hydrate 的陈旧态
    /// 重置语义另测（hydrate_resets_stale_runtime_fields）。
    #[test]
    fn snapshot_roundtrip() {
        let dir = std::env::temp_dir().join("claude-runtime-snapshot-test");
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("snapshot.json");

        let store = ClaudeRuntimeStore::default();
        store
            .0
            .lock()
            .unwrap()
            .insert("issue1::main".to_string(), sample_state());
        persist_to(&store, &path);

        // 快照文件本体保留全部字段（含 launch_token/status）。
        let raw: HashMap<String, ClaudeRuntimeState> =
            serde_json::from_str(&std::fs::read_to_string(&path).unwrap()).unwrap();
        let saved = &raw["issue1::main"];
        assert_eq!(saved.launch_token.as_deref(), Some("tok1"));
        assert_eq!(saved.status, ClaudeRuntimeStatus::Working);
        assert_eq!(saved.preview_text.as_deref(), Some("thinking..."));

        // hydrate 后走重置语义：绑定字段保留，运行时字段清空。
        let store2 = ClaudeRuntimeStore::default();
        hydrate_from(&store2, &path);
        let map = store2.0.lock().unwrap();
        assert_eq!(map.len(), 1);
        let s = &map["issue1::main"];
        assert_eq!(s.claude_session_id.as_deref(), Some("sess1"));
        assert_eq!(s.transcript_path.as_deref(), Some("/tmp/t.jsonl"));
        assert_eq!(s.status, ClaudeRuntimeStatus::Idle);
        assert_eq!(s.launch_token, None);

        let _ = std::fs::remove_dir_all(&dir);
    }

    /// hydrate 陈旧态重置：working/waiting 恢复为 idle，token/preview/notification
    /// 清空，session_id/transcript_path 保留。
    #[test]
    fn hydrate_resets_stale_runtime_fields() {
        let dir = std::env::temp_dir().join("claude-runtime-hydrate-reset-test");
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("snapshot.json");

        let store = ClaudeRuntimeStore::default();
        store
            .0
            .lock()
            .unwrap()
            .insert("issue1::main".to_string(), sample_state());
        persist_to(&store, &path);

        let store2 = ClaudeRuntimeStore::default();
        hydrate_from(&store2, &path);
        let s = &store2.0.lock().unwrap()["issue1::main"];
        assert_eq!(s.status, ClaudeRuntimeStatus::Idle);
        assert_eq!(s.launch_token, None);
        assert_eq!(s.preview_text, None);
        assert_eq!(s.notification, None);
        // 会话绑定保留（resume 与 transcript 定位用）。
        assert_eq!(s.claude_session_id.as_deref(), Some("sess1"));
        assert_eq!(s.transcript_path.as_deref(), Some("/tmp/t.jsonl"));

        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn hydrate_missing_or_corrupt_is_noop() {
        let store = ClaudeRuntimeStore::default();
        hydrate_from(
            &store,
            Path::new("/nonexistent/definitely/snapshot.json"),
        );
        assert!(store.0.lock().unwrap().is_empty());

        let dir = std::env::temp_dir().join("claude-runtime-corrupt-test");
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("bad.json");
        std::fs::write(&path, "not json").unwrap();
        hydrate_from(&store, &path);
        assert!(store.0.lock().unwrap().is_empty());
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn state_to_payload_maps_fields() {
        let s = sample_state();
        let p = s.to_payload("issue1::main");
        assert_eq!(p.pane, "issue1::main");
        assert_eq!(p.status, ClaudeRuntimeStatus::Working);
        assert_eq!(p.claude_session_id.as_deref(), Some("sess1"));
        assert_eq!(p.last_updated_at, 123_456);
        assert_eq!(
            p.notification
                .as_ref()
                .unwrap()
                .tool_name
                .as_deref(),
            Some("Bash")
        );
    }
}
