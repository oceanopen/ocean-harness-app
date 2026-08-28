// ClaudeRuntimeStore：pane → ClaudeRuntimeState 的运行时状态存储（hook 事件唯一状态源）。
//
// 范式对齐 claude_sessions.rs：`pub struct X(pub Mutex<HashMap<..>>)` +
// `#[derive(Default)]` + init 时 app.manage。锁 poison 走 expect panic 兜底
// （短临界区，poison 只在持锁线程 panic 时发生）。
//
// 快照持久化：store 变更时 persist 覆写 app_data_dir/claude_runtime_snapshot.json，
// 启动 init hydrate——支撑 app 重启后 resume（T5.2）。
//
// chat 模式退役裁剪（2026-08）：state 收缩为会话绑定链（launch_token/
// claude_session_id/transcript_path/updated_at）；旧快照多余键（status/
// previewText/previewIndex/notification）serde 默认忽略，无需迁移。

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::{Mutex, OnceLock};

use serde::{Deserialize, Serialize};
use tauri::{App, Manager};

use super::types::ClaudeRuntimeChangedPayload;

/// 单 pane 的运行时状态（store value，内部类型，不 specta 导出）。
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ClaudeRuntimeState {
    /// 本次 spawn 的代际标（env WE_TERM_LAUNCH_TOKEN，T1.4 注入）；SessionStart
    /// 绑定，陈旧事件围栏用（T1.3；chat 退役后仅 SessionStart 进 Apply，围栏
    /// 作为换代语义的显式防线保留）。
    pub launch_token: Option<String>,
    /// claude 会话 ID（resume 用，T5.2「重开并启动 claude」）。
    pub claude_session_id: Option<String>,
    /// transcript JSONL 绝对路径（SessionStart 绑定）。
    pub transcript_path: Option<String>,
    /// 最后更新时间（毫秒时间戳）。
    pub updated_at: i64,
}

impl ClaudeRuntimeState {
    /// 映射为前端事件 payload（加 pane，去内部 launch_token）。T1.3 emit /
    /// claude_runtime_state 命令（resume 查询）复用。
    pub fn to_payload(&self, pane: &str) -> ClaudeRuntimeChangedPayload {
        ClaudeRuntimeChangedPayload {
            pane: pane.to_string(),
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

/// store 变更后落盘（ingest 更新后调用）。序列化在锁内、IO 在锁外，
/// 避免写盘阻塞其它读写方。
pub fn persist(store: &ClaudeRuntimeStore) {
    let Some(path) = SNAPSHOT_PATH.get() else {
        return;
    };
    persist_to(store, path);
}

/// 读快照填充 store（启动 hydrate）。文件缺失/损坏静默跳过（不 panic，空态启动）。
///
/// 陈旧态重置：app 重启后 claude 实际状态未知，每条恢复的 state 清空
/// launch_token（等新 SessionStart 重新绑定换代）；claude_session_id/
/// transcript_path 保留（resume 依赖，T5.2）。
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
            s.launch_token = None;
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

        // 快照文件本体保留全部字段（含 launch_token）。
        let raw: HashMap<String, ClaudeRuntimeState> =
            serde_json::from_str(&std::fs::read_to_string(&path).unwrap()).unwrap();
        let saved = &raw["issue1::main"];
        assert_eq!(saved.launch_token.as_deref(), Some("tok1"));
        assert_eq!(saved.claude_session_id.as_deref(), Some("sess1"));

        // hydrate 恢复条目（重置语义的逐字段断言见 hydrate_resets）。
        let store2 = ClaudeRuntimeStore::default();
        hydrate_from(&store2, &path);
        assert_eq!(store2.0.lock().unwrap().len(), 1);

        let _ = std::fs::remove_dir_all(&dir);
    }

    /// 旧格式快照兼容（chat 退役裁剪）：含已删除字段（status/previewText/
    /// previewIndex/notification）的快照 hydrate 成功，多余键静默忽略，
    /// 绑定字段照常恢复——无需迁移。
    #[test]
    fn hydrate_tolerates_legacy_snapshot_fields() {
        let dir = std::env::temp_dir().join("claude-runtime-legacy-snapshot-test");
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("snapshot.json");
        std::fs::write(
            &path,
            r#"{
  "issue1::main": {
    "launchToken": "tok-old",
    "claudeSessionId": "sess-old",
    "transcriptPath": "/tmp/old.jsonl",
    "status": "working",
    "previewText": "生成中",
    "previewIndex": 3,
    "notification": {"message": "approve", "toolName": "Bash"},
    "updatedAt": 99
  }
}"#,
        )
        .unwrap();

        let store = ClaudeRuntimeStore::default();
        hydrate_from(&store, &path);
        let map = store.0.lock().unwrap();
        assert_eq!(map.len(), 1);
        let s = &map["issue1::main"];
        assert_eq!(s.claude_session_id.as_deref(), Some("sess-old"));
        assert_eq!(
            s.transcript_path.as_deref(),
            Some("/tmp/old.jsonl")
        );
        assert_eq!(
            s.launch_token, None,
            "hydrate 清 token（等新 SessionStart 重绑）"
        );

        let _ = std::fs::remove_dir_all(&dir);
    }

    /// hydrate 陈旧态重置：token 清空，session_id/transcript_path 保留（resume 用）。
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
        assert_eq!(s.launch_token, None);
        // 会话绑定保留（resume 用）。
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
        assert_eq!(p.claude_session_id.as_deref(), Some("sess1"));
        assert_eq!(p.transcript_path.as_deref(), Some("/tmp/t.jsonl"));
        assert_eq!(p.last_updated_at, 123_456);
    }
}
