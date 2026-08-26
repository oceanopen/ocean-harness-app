// 工作区 hooks 安装器（T1.2）：<workspace>/.claude/settings.json 幂等合并。
//
// orca writeHooksJson 范式（installer-utils.ts）全量照抄：
//   - 合并不覆盖：只识别自有条目（command 含 needle `claude-hooks/hook.sh`）
//     做替换升级，用户 hooks 条目与其他顶层键（permissions 等）原样保留；
//     needle 用文件名而非精确路径——dev/release 双实例 app_data_dir 不同，
//     换实例安装也要能扫掉旧实例的陈旧条目。
//   - 内容相同跳过：防反复 install 滚掉 .bak（最后一份可恢复副本）。
//   - temp+rename 原子写 + 滚动 .bak 单备份（拒绝符号链接目标，防 copy 跟链
//     损坏无关 dotfile）。
//   - 脚本先于 settings 落盘（settings 指向的脚本不能缺席）。
//
// 事件注册集（8 事件，依据 orca 最新 hook-settings.ts + claude 2.1.231 官方
// hooks 文档核查，见 docs/claude_orca_mode_02_tasks.md T1.2 / T4.1）：
//   SessionStart / UserPromptSubmit / MessageDisplay / PreToolUse / Stop /
//   StopFailure / Notification（无 matcher）+ PermissionRequest（matcher "*"）。
//   PreToolUse（T4.1）无 matcher：ingest 侧只让 AskUserQuestion 进状态机
//   （提问卡数据源），普通工具调用高频全量 Drop。

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use serde_json::Value;

use super::script::{ensure_hook_script, hook_command};

/// 注册的 hook 事件（有序，输出 JSON 稳定）：(事件名, matcher)。
/// matcher 为 None 的 events 写 matcher 会被 claude 静默忽略，索性不写。
const HOOK_EVENTS: &[(&str, Option<&str>)] = &[
    ("SessionStart", None),
    ("UserPromptSubmit", None),
    ("MessageDisplay", None),
    ("PreToolUse", None),
    ("Stop", None),
    ("StopFailure", None),
    ("PermissionRequest", Some("*")),
    ("Notification", None),
];

/// 自有条目识别 needle：脚本相对路径段（文件名级，非完整路径）。
const MANAGED_NEEDLE: &str = "claude-hooks/hook.sh";

/// hook handler 超时秒数（orca MANAGED_HOOK_TIMEOUT_SECONDS 同值）。
const MANAGED_HOOK_TIMEOUT_SECS: u64 = 10;

/// settings.json 模型：hooks 子树结构化处理，其余顶层键 flatten 原样保留
/// （serde 序列化 BTreeMap 键序稳定，用户键不丢不重命名）。
#[derive(Debug, Default, Serialize, Deserialize)]
struct SettingsModel {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    hooks: Option<BTreeMap<String, Vec<HookDefinition>>>,
    #[serde(flatten)]
    extra: BTreeMap<String, Value>,
}

/// 单个 hook definition（事件下的 matcher 组）。用户条目字段不定型
/// （matcher/hooks/command/timeout/…），Value 宽容承载；仅 command 相关
/// 字段结构化读取供识别。
#[derive(Debug, Clone, Serialize, Deserialize)]
struct HookDefinition {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    matcher: Option<Value>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    hooks: Option<Vec<HookHandler>>,
    /// 用户条目可能是直接 command 形态（Cursor 式）而非嵌套 hooks——宽容保留。
    #[serde(default, skip_serializing_if = "Option::is_none")]
    command: Option<String>,
    #[serde(flatten)]
    extra: BTreeMap<String, Value>,
}

/// definition 内嵌 handler（{type, command, timeout}）。
#[derive(Debug, Clone, Serialize, Deserialize)]
struct HookHandler {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    r#type: Option<Value>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    command: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    timeout: Option<Value>,
    #[serde(flatten)]
    extra: BTreeMap<String, Value>,
}

/// definition 是否自有（嵌套 hooks 任一 command 或直接 command 含 needle）。
fn is_managed_definition(def: &HookDefinition) -> bool {
    if let Some(cmd) = &def.command {
        if cmd.contains(MANAGED_NEEDLE) {
            return true;
        }
    }
    def.hooks.as_ref().is_some_and(|hs| {
        hs.iter().any(|h| {
            h.command
                .as_deref()
                .is_some_and(|c| c.contains(MANAGED_NEEDLE))
        })
    })
}

/// 剥自有条目：definition 内过滤嵌套 managed handler；剥空后 definition
/// 无任何 command 形态则整条移除；纯用户 definition 原样保留。
fn strip_managed(definitions: &[HookDefinition]) -> Vec<HookDefinition> {
    definitions
        .iter()
        .filter_map(|def| {
            if !is_managed_definition(def) {
                return Some(def.clone());
            }
            let mut next = def.clone();
            if let Some(hs) = &next.hooks {
                let kept: Vec<HookHandler> = hs
                    .iter()
                    .filter(|h| {
                        !h.command
                            .as_deref()
                            .is_some_and(|c| c.contains(MANAGED_NEEDLE))
                    })
                    .cloned()
                    .collect();
                if kept.is_empty() {
                    next.hooks = None;
                } else {
                    next.hooks = Some(kept);
                }
            }
            // 直接 command 形态的 managed definition：command 一并清。
            if next
                .command
                .as_deref()
                .is_some_and(|c| c.contains(MANAGED_NEEDLE))
            {
                next.command = None;
            }
            let has_command_left =
                next.command.is_some() || next.hooks.as_ref().is_some_and(|h| !h.is_empty());
            if has_command_left {
                Some(next)
            } else {
                None
            }
        })
        .collect()
}

/// 构建自有 definition（每事件一条）。
fn managed_definition(command: &str, matcher: Option<&str>) -> HookDefinition {
    HookDefinition {
        matcher: matcher.map(|m| Value::String(m.to_string())),
        hooks: Some(vec![HookHandler {
            r#type: Some(Value::String("command".into())),
            command: Some(command.to_string()),
            timeout: Some(Value::from(MANAGED_HOOK_TIMEOUT_SECS)),
            extra: BTreeMap::new(),
        }]),
        command: None,
        extra: BTreeMap::new(),
    }
}

/// 合并：每注册事件剥自有条目后尾插新 definition；未注册事件的用户条目
/// 整体不动。返回合并后的 settings 模型。
fn merge_hooks(mut model: SettingsModel, command: &str) -> SettingsModel {
    let hooks = model.hooks.get_or_insert_with(BTreeMap::new);
    for (event, matcher) in HOOK_EVENTS {
        let existing = hooks.get(*event).cloned().unwrap_or_default();
        let mut merged = strip_managed(&existing);
        merged.push(managed_definition(command, *matcher));
        hooks.insert(event.to_string(), merged);
    }
    model
}

/// 读 settings.json → 模型。文件缺失返回空模型；解析失败也返回空模型起步
/// （warn 不阻塞——损坏文件留给 .bak 兜底，安装动作本身是修复路径）。
fn read_settings(path: &Path) -> SettingsModel {
    match std::fs::read_to_string(path) {
        Ok(content) => match serde_json::from_str(&content) {
            Ok(m) => m,
            Err(e) => {
                log::warn!(
                    "[claude_runtime] settings.json parse failed, starting fresh: {} ({e})",
                    path.display()
                );
                SettingsModel::default()
            }
        },
        Err(_) => SettingsModel::default(),
    }
}

/// 序列化格式：pretty + 尾换行（对齐 orca，diff 友好且与跳过比较一致）。
fn serialize_settings(model: &SettingsModel) -> String {
    let mut json = serde_json::to_string_pretty(model).expect("settings model serializable");
    json.push('\n');
    json
}

/// 滚动单备份：现文件 → <path>.bak（copy 经 tmp+rename，拒绝符号链接目标）。
fn write_rolling_backup(source: &Path) -> Result<(), String> {
    let backup = source.with_extension("json.bak");
    let meta = std::fs::symlink_metadata(source)
        .map_err(|e| format!("stat {} failed: {e}", source.display()))?;
    if meta.file_type().is_symlink() {
        return Err(format!(
            "refusing symlinked settings: {}",
            source.display()
        ));
    }
    // tmp 名带 pid+纳秒：同毫秒并发 install 不互踩（对齐 orca randomUUID 防碰撞）。
    let tmp = backup.with_extension(format!(
        "json.bak.{}.{}.tmp",
        std::process::id(),
        super::script::nanos_suffix()
    ));
    std::fs::copy(source, &tmp)
        .map_err(|e| format!("backup copy {} failed: {e}", source.display()))?;
    std::fs::rename(&tmp, &backup).map_err(|e| format!("backup rename failed: {e}"))
}

/// 安装入口：脚本落盘 + settings 合并写入。幂等（内容相同零写）。
/// pub(crate)：无 AppHandle 的核心逻辑，命令入口与集成测试（T1.4 真 claude e2e）共用。
pub(crate) fn install(workspace_dir: &Path, base_dir: &Path) -> Result<(), String> {
    // 1. 脚本先于 settings（settings 指向的脚本不能缺席）。
    let script_path = ensure_hook_script(base_dir)?;
    let command = hook_command(&script_path);

    // 2. 读现配置 → 合并。
    let settings_path = workspace_dir
        .join(".claude")
        .join("settings.json");
    let model = read_settings(&settings_path);
    let merged = merge_hooks(model, &command);
    let serialized = serialize_settings(&merged);

    // 3. 内容相同跳过（不滚 .bak）。
    if let Ok(existing) = std::fs::read_to_string(&settings_path) {
        if existing == serialized {
            return Ok(());
        }
    }

    // 4. 原子写：.bak 滚动备份 → temp+rename。
    if let Some(parent) = settings_path.parent() {
        std::fs::create_dir_all(parent)
            .map_err(|e| format!("mkdir {} failed: {e}", parent.display()))?;
    }
    if settings_path.exists() {
        write_rolling_backup(&settings_path)?;
    }
    let tmp = settings_path.with_extension(format!(
        "json.{}.{}.tmp",
        std::process::id(),
        super::script::nanos_suffix()
    ));
    std::fs::write(&tmp, &serialized)
        .map_err(|e| format!("write {} failed: {e}", tmp.display()))?;
    std::fs::rename(&tmp, &settings_path)
        .map_err(|e| format!("rename {} failed: {e}", settings_path.display()))?;
    Ok(())
}

/// Tauri 命令：前端 spawn 前调用（幂等）。cwd = 工作区目录
/// （`${workspace_base_dir}/${issueId}`，usePtySession 派生先例）。
#[tauri::command]
#[specta::specta]
pub fn ensure_workspace_hooks(app: tauri::AppHandle, cwd: String) -> Result<(), String> {
    use tauri::Manager;
    let base_dir: PathBuf = app
        .path()
        .app_data_dir()
        .map_err(|e| format!("resolve app_data_dir failed: {e}"))?;
    install(Path::new(&cwd), &base_dir)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn managed_cmd(path: &str) -> String {
        hook_command(Path::new(path))
    }

    fn parse_definitions(json: &str) -> Vec<HookDefinition> {
        serde_json::from_str(json).unwrap()
    }

    fn workspace_fixture() -> PathBuf {
        // 调用方传子目录名：并行测试各自独立临时目录（共享目录名曾致跨测试竞态）。
        let thread_name = std::thread::current()
            .name()
            .unwrap_or("anon")
            .to_string();
        let name = thread_name.rsplit("::").next().unwrap_or("anon");
        let dir = std::env::temp_dir().join(format!("claude-runtime-installer-{name}"));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(dir.join(".claude")).unwrap();
        dir
    }

    /// 手动工具（T2.1 dev 实测用；T6.1 前端接入 spawn 前自动安装后可删）：
    /// 给 dev app 真实 app_data_dir + 指定工作区装 hooks（脚本 + settings 合并）。
    /// env 驱动：`WE_E2E_BASE=<app_data_dir> WE_E2E_WS=<工作区> \
    /// cargo test install_hooks_for_dev_e2e -- --ignored --manifest-path src-tauri/Cargo.toml`
    #[test]
    #[ignore = "手动工具：需 WE_E2E_BASE/WE_E2E_WS 环境变量"]
    fn install_hooks_for_dev_e2e() {
        let base = std::env::var("WE_E2E_BASE").expect("WE_E2E_BASE not set");
        let ws = std::env::var("WE_E2E_WS").expect("WE_E2E_WS not set");
        install(Path::new(&ws), Path::new(&base)).expect("install failed");
    }

    #[test]
    fn merge_keeps_user_entries_and_appends_managed() {
        // 用户已有 Stop hook 与 PreToolUse hook：合并后共存，自有条目追加。
        let user_stop =
            r#"[{"matcher":"","hooks":[{"type":"command","command":"echo user-stop"}]}]"#;
        let mut hooks = BTreeMap::new();
        hooks.insert("Stop".to_string(), parse_definitions(user_stop));
        hooks.insert(
            "PreToolUse".to_string(),
            parse_definitions(
                r#"[{"matcher":"Bash","hooks":[{"type":"command","command":"echo user-pre"}]}]"#,
            ),
        );
        let model = SettingsModel {
            hooks: Some(hooks),
            extra: {
                let mut m = BTreeMap::new();
                m.insert(
                    "permissions".to_string(),
                    json!({"allow": ["Bash"]}),
                );
                m
            },
        };

        let merged = merge_hooks(model, &managed_cmd("/app/claude-hooks/hook.sh"));
        let hooks = merged.hooks.unwrap();

        // Stop：用户 1 条 + 自有 1 条。
        assert_eq!(hooks["Stop"].len(), 2);
        assert_eq!(
            hooks["Stop"][0].hooks.as_ref().unwrap()[0]
                .command
                .as_deref(),
            Some("echo user-stop")
        );
        assert!(
            hooks["Stop"][1].hooks.as_ref().unwrap()[0]
                .command
                .as_deref()
                .unwrap()
                .contains("claude-hooks/hook.sh")
        );

        // PreToolUse（T4.1 已注册）：用户 matcher 条目 + 自有条目共存。
        assert_eq!(hooks["PreToolUse"].len(), 2);
        assert_eq!(
            hooks["PreToolUse"][0].matcher,
            Some(Value::String("Bash".into()))
        );
        assert!(
            hooks["PreToolUse"][1].hooks.as_ref().unwrap()[0]
                .command
                .as_deref()
                .unwrap()
                .contains("claude-hooks/hook.sh")
        );

        // 8 注册事件全部在场；PermissionRequest 带 matcher "*"。
        assert_eq!(
            hooks["PermissionRequest"][0].matcher,
            Some(Value::String("*".into()))
        );
        assert_eq!(hooks["SessionStart"][0].matcher, None);
        for event in [
            "SessionStart",
            "UserPromptSubmit",
            "MessageDisplay",
            "PreToolUse",
            "Stop",
            "StopFailure",
            "PermissionRequest",
            "Notification",
        ] {
            assert!(hooks.contains_key(event), "missing {event}");
        }

        // 顶层用户键保留。
        assert_eq!(
            merged.extra["permissions"],
            json!({"allow": ["Bash"]})
        );
    }

    #[test]
    fn merge_replaces_stale_managed_not_duplicates() {
        // 旧实例路径的自有条目（dev → release 切换）被 needle 扫掉并替换为新 command。
        let stale = format!(
            r#"[{{"hooks":[{{"type":"command","command":"{}"}}]}}]"#,
            managed_cmd("/old-dev-dir/claude-hooks/hook.sh")
        );
        let mut hooks = BTreeMap::new();
        hooks.insert("Stop".to_string(), parse_definitions(&stale));
        let model = SettingsModel {
            hooks: Some(hooks),
            extra: BTreeMap::new(),
        };

        let merged = merge_hooks(
            model,
            &managed_cmd("/new-release-dir/claude-hooks/hook.sh"),
        );
        let stop = merged.hooks.unwrap()["Stop"].clone();
        assert_eq!(stop.len(), 1);
        assert!(
            stop[0].hooks.as_ref().unwrap()[0]
                .command
                .as_deref()
                .unwrap()
                .contains("/new-release-dir/")
        );
    }

    #[test]
    fn strip_removes_managed_keeps_user_sibling_handler() {
        // 同一 definition 内用户 handler 与自有 handler 并存：只剥自有。
        let def = format!(
            r#"[{{"hooks":[{{"type":"command","command":"echo user"}},{{"type":"command","command":"{}"}}]}}]"#,
            managed_cmd("/app/claude-hooks/hook.sh")
        );
        let stripped = strip_managed(&parse_definitions(&def));
        assert_eq!(stripped.len(), 1);
        assert_eq!(stripped[0].hooks.as_ref().unwrap().len(), 1);
        assert_eq!(
            stripped[0].hooks.as_ref().unwrap()[0]
                .command
                .as_deref(),
            Some("echo user")
        );
    }

    #[test]
    fn install_full_flow_idempotent_with_backup() {
        let ws = workspace_fixture();
        let base = ws.join("appdata");
        let settings = ws.join(".claude").join("settings.json");

        // 首装：从零生成，无 .bak（原本无文件）。
        install(&ws, &base).unwrap();
        assert!(settings.exists());
        assert!(!settings.with_extension("json.bak").exists());
        let first = std::fs::read_to_string(&settings).unwrap();
        assert!(first.contains("UserPromptSubmit"));
        assert!(first.contains("PermissionRequest"));

        // 用户手工加条目（模拟用户后续编辑）→ 二次安装：合并保留 + .bak 滚动。
        let mut doc: serde_json::Value = serde_json::from_str(&first).unwrap();
        doc["permissions"] = json!({"allow": ["WebSearch"]});
        std::fs::write(
            &settings,
            serde_json::to_string_pretty(&doc).unwrap(),
        )
        .unwrap();
        install(&ws, &base).unwrap();
        let second = std::fs::read_to_string(&settings).unwrap();
        assert!(second.contains("WebSearch"));
        assert!(second.contains("claude-hooks/hook.sh"));
        assert!(
            settings.with_extension("json.bak").exists(),
            "backup rolled"
        );

        // 三次安装：内容相同跳过（.bak 内容不滚，仍是上一版）。
        let bak_before = std::fs::read_to_string(settings.with_extension("json.bak")).unwrap();
        install(&ws, &base).unwrap();
        assert_eq!(
            std::fs::read_to_string(&settings).unwrap(),
            second,
            "identical content skipped"
        );
        assert_eq!(
            std::fs::read_to_string(settings.with_extension("json.bak")).unwrap(),
            bak_before,
            "backup not rolled on skip"
        );

        let _ = std::fs::remove_dir_all(&ws);
    }

    #[test]
    fn install_recovers_from_corrupt_settings() {
        let ws = workspace_fixture();
        let base = ws.join("appdata");
        let settings = ws.join(".claude").join("settings.json");
        std::fs::write(&settings, "not json").unwrap();

        install(&ws, &base).unwrap();
        let content = std::fs::read_to_string(&settings).unwrap();
        assert!(content.contains("SessionStart"));
        // 损坏原文进了 .bak（可人工找回）。
        assert_eq!(
            std::fs::read_to_string(settings.with_extension("json.bak")).unwrap(),
            "not json"
        );
        let _ = std::fs::remove_dir_all(&ws);
    }

    #[test]
    fn install_writes_script_before_settings() {
        // settings 指向的脚本必须先在场：install 成功即脚本存在且可执行。
        let ws = workspace_fixture();
        let base = ws.join("appdata");
        install(&ws, &base).unwrap();
        let script = base.join("claude-hooks").join("hook.sh");
        assert!(script.exists());
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = std::fs::metadata(&script)
                .unwrap()
                .permissions()
                .mode();
            assert_eq!(mode & 0o111, 0o111);
        }
        let _ = std::fs::remove_dir_all(&ws);
    }
}
