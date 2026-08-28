// 工作区 hooks 安装器（T1.2 + T6.1 加固）：<workspace>/.claude/settings.json 幂等合并。
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
// T6.1 加固（T1.2 审查遗留 A/B，方案经确认：保守保留 + 仅语义比较）：
//   A 保守保留——serde_json::Value 层手动遍历，只动认识的部分（8 注册事件的
//     自有条目），不认识的形态（hooks 非对象 / 事件值非数组 / definition 非
//     对象等）原样透传 + warn + 跳过该处安装，任意形态都保得住用户内容。
//     旧实现整文档反序列化到 struct 模型，失败面是全文件——hooks 子树形态
//     意外（合法 JSON）会把用户顶层键与 hooks 条目整文件丢弃。语法损坏
//     （非法 JSON）仍空起步 + .bak 兜底（安装即修复路径，T1.2 语义不变）。
//   B 语义比较跳过——磁盘内容 parse 后与合并产物 Value 相等（键序/格式/空白
//     无关）即零写零备份。旧实现磁盘原文与 to_string_pretty 产物逐字节比较，
//     claude 自身改写 settings（插入序/格式）后跳过必然失效、.bak 反复滚动。
//     注：serde_json 未启用 preserve_order，Value 写盘仍按 Map 字母序——
//     键序保留放弃（仅重写时的 diff 美观代价，语义比较已根治跳过失效）。
//
// 事件注册集（8 事件，依据 orca 最新 hook-settings.ts + claude 2.1.231 官方
// hooks 文档核查，见 docs/claude_orca_mode_02_tasks.md T1.2 / T4.1）：
//   SessionStart / UserPromptSubmit / MessageDisplay / PreToolUse / Stop /
//   StopFailure / Notification（无 matcher）+ PermissionRequest（matcher "*"）。
//   PreToolUse（T4.1）无 matcher：ingest 侧只让 AskUserQuestion 进状态机
//   （提问卡数据源），普通工具调用高频全量 Drop。

use std::path::{Path, PathBuf};

use serde_json::{Map, Value};

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

/// definition/handler 是否自有：direct command（Cursor 式）或嵌套 hooks 数组
/// 任一 handler 的 command 含 needle。definition 与 handler 同为「带 command
/// 字段的对象」，一个判定两用；非对象元素天然不命中（保守透传）。
fn has_managed_command(v: &Value) -> bool {
    v.get("command")
        .and_then(Value::as_str)
        .is_some_and(|c| c.contains(MANAGED_NEEDLE))
}

/// definition 是否自有（direct command 或嵌套 handler 任一含 needle）。
fn is_managed_definition(def: &Value) -> bool {
    has_managed_command(def)
        || def
            .get("hooks")
            .and_then(Value::as_array)
            .is_some_and(|hs| hs.iter().any(has_managed_command))
}

/// 剥自有条目：非自有 definition 原样保留；自有 definition 内过滤嵌套
/// managed handler、清 direct command，剥完无任何 command 形态则整条移除
/// （同 definition 内用户 handler 兄弟保留，残躯只含用户 handler 的留存）。
fn strip_managed_definitions(definitions: &[Value]) -> Vec<Value> {
    definitions
        .iter()
        .filter_map(|def| {
            if !is_managed_definition(def) {
                return Some(def.clone());
            }
            // clone 前判定 direct command（规避 obj 可变借用期间的不可变借用）。
            let direct_managed = has_managed_command(def);
            let mut next = def.clone();
            let Some(obj) = next.as_object_mut() else {
                // is_managed_definition 命中即含 command/hooks 字段，必为对象；
                // 防御分支，不可达。
                return Some(next);
            };
            // 嵌套 handlers 原地剥自有（用户 handler 兄弟保留）；剥空移除键。
            // 非数组 hooks 的 and_then 短路 → 原样不动（畸形用户值透传）。
            let hooks_drained_empty = obj
                .get_mut("hooks")
                .and_then(Value::as_array_mut)
                .is_some_and(|hs| {
                    hs.retain(|h| !has_managed_command(h));
                    hs.is_empty()
                });
            if hooks_drained_empty {
                obj.remove("hooks");
            }
            if direct_managed {
                obj.remove("command");
            }
            let has_command_left = next.get("command").is_some()
                || next
                    .get("hooks")
                    .and_then(Value::as_array)
                    .is_some_and(|h| !h.is_empty());
            if has_command_left {
                Some(next)
            } else {
                None
            }
        })
        .collect()
}

/// 构建自有 definition（每事件一条）。写盘键序由 serde_json Map 字母序决定
/// （未启用 preserve_order，见文件头注 B——插入序不影响输出）。
fn managed_definition(command: &str, matcher: Option<&str>) -> Value {
    let mut handler = Map::new();
    handler.insert(
        "type".to_string(),
        Value::String("command".into()),
    );
    handler.insert(
        "command".to_string(),
        Value::String(command.to_string()),
    );
    handler.insert(
        "timeout".to_string(),
        Value::from(MANAGED_HOOK_TIMEOUT_SECS),
    );
    let mut def = Map::new();
    if let Some(m) = matcher {
        def.insert(
            "matcher".to_string(),
            Value::String(m.to_string()),
        );
    }
    def.insert(
        "hooks".to_string(),
        Value::Array(vec![Value::Object(handler)]),
    );
    Value::Object(def)
}

/// hooks 子树合并（保守保留）：object 逐事件剥自有尾插新条目；未注册事件键
/// 整体不动；事件值非数组 → 原样保留 + warn + 跳过该事件；hooks 非 object 且
/// 非 null → 整树不动 + warn（合并零变化，调用方语义比较自然零写）。
/// root 须为对象（调用方保证；非对象时静默不动，warn 由调用方记）。
fn merge_managed_hooks(root: &mut Value, command: &str) {
    let Some(obj) = root.as_object_mut() else {
        return;
    };
    let mut hooks: Map<String, Value> = match obj.get("hooks") {
        // 缺失或 null（无用户内容可破坏）：从空 map 起步正常安装。
        None | Some(Value::Null) => Map::new(),
        Some(v) => match v.as_object() {
            Some(m) => m.clone(),
            None => {
                log::warn!(
                    "[claude_runtime] settings.json hooks subtree is not an object, skipping hook install: {v:?}"
                );
                return;
            }
        },
    };
    for (event, matcher) in HOOK_EVENTS {
        let mut merged = match hooks.get(*event) {
            Some(Value::Array(definitions)) => strip_managed_definitions(definitions),
            None => Vec::new(),
            // 保守保留：形态意外的事件值原样保留 + 跳过该事件安装。
            Some(other) => {
                log::warn!(
                    "[claude_runtime] settings.json hooks.{event} is not an array, skipping event install: {other:?}"
                );
                continue;
            }
        };
        merged.push(managed_definition(command, *matcher));
        hooks.insert(event.to_string(), Value::Array(merged));
    }
    obj.insert("hooks".to_string(), Value::Object(hooks));
}

/// 序列化格式：pretty + 尾换行（对齐 orca，diff 友好）。
fn serialize_value(value: &Value) -> String {
    let mut json = serde_json::to_string_pretty(value).expect("settings value serializable");
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

/// 安装入口：脚本落盘 + settings 合并写入。幂等（语义相同零写）。
/// pub(crate)：无 AppHandle 的核心逻辑，命令入口与集成测试共用。
pub(crate) fn install(workspace_dir: &Path, base_dir: &Path) -> Result<(), String> {
    // 1. 脚本先于 settings（settings 指向的脚本不能缺席）。
    let script_path = ensure_hook_script(base_dir)?;
    let command = hook_command(&script_path);

    // 2. 读现配置（一次 parse 两用：合并输入 + 跳过比较基准）。语法损坏 /
    //    文件缺失 → None（空起步；损坏原文由 .bak 兜底，安装即修复路径）。
    let settings_path = workspace_dir
        .join(".claude")
        .join("settings.json");
    let parsed: Option<Value> = match std::fs::read_to_string(&settings_path) {
        Ok(content) => match serde_json::from_str(&content) {
            Ok(v) => Some(v),
            Err(e) => {
                log::warn!(
                    "[claude_runtime] settings.json parse failed, starting fresh: {} ({e})",
                    settings_path.display()
                );
                None
            }
        },
        Err(_) => None,
    };

    // 3. 合并（保守保留）：对象 → 逐事件合并（match 借用 + clone，parsed 留作
    //    跳过比较基准；settings.json 体量小，无谓开销）；合法 JSON 非对象 →
    //    整体透传不动 + warn + 跳过安装（零写零备份，安装降级但不破坏）。
    let merged = match &parsed {
        Some(root) if root.is_object() => {
            let mut root = root.clone();
            merge_managed_hooks(&mut root, &command);
            root
        }
        None => {
            let mut fresh = Value::Object(Map::new());
            merge_managed_hooks(&mut fresh, &command);
            fresh
        }
        Some(_) => {
            log::warn!(
                "[claude_runtime] settings.json is valid JSON but not an object, skipping hook install: {}",
                settings_path.display()
            );
            return Ok(());
        }
    };

    // 4. 语义比较跳过（不滚 .bak）：磁盘内容 parse 后与合并产物 Value 相等
    //    （键序/格式/空白无关）→ 零写。claude 自身改写（插入序/缩进差异）
    //    不再触发误写与备份滚动。
    if let Some(disk) = &parsed {
        if *disk == merged {
            return Ok(());
        }
    }

    // 5. 原子写：.bak 滚动备份 → temp+rename。
    if let Some(parent) = settings_path.parent() {
        std::fs::create_dir_all(parent)
            .map_err(|e| format!("mkdir {} failed: {e}", parent.display()))?;
    }
    if settings_path.exists() {
        write_rolling_backup(&settings_path)?;
    }
    let serialized = serialize_value(&merged);
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

    fn workspace_fixture() -> PathBuf {
        // 以测试线程名派生子目录名：并行测试各自独立临时目录（共享固定目录名
        // 曾致跨测试竞态；Rust 默认测试线程名 = 测试函数名，天然唯一）。
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

    fn read_settings_value(settings: &Path) -> Value {
        serde_json::from_str(&std::fs::read_to_string(settings).unwrap()).unwrap()
    }

    /// needle 与脚本目录/文件名契约钉死（script.rs env 名单测同款惯例）：脚本
    /// 改名而 needle 未跟 → 自有条目识别失效 → 每次 spawn 追加重复 definition。
    #[test]
    fn managed_needle_matches_script_constants() {
        assert_eq!(
            MANAGED_NEEDLE,
            format!(
                "{}/{}",
                super::super::script::HOOK_SCRIPT_DIR_NAME,
                super::super::script::HOOK_SCRIPT_FILE_NAME
            )
        );
    }

    #[test]
    fn merge_keeps_user_entries_and_appends_managed() {
        // 用户已有 Stop/PreToolUse hook 与未注册事件键：合并后共存，自有条目
        // 追加，未注册键与顶层 permissions 整体不动。
        let mut root = json!({
            "permissions": {"allow": ["Bash"]},
            "hooks": {
                "Stop": [{"matcher": "", "hooks": [{"type": "command", "command": "echo user-stop"}]}],
                "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo user-pre"}]}],
                "CustomEvent": [{"hooks": [{"type": "command", "command": "echo user-custom"}]}]
            }
        });

        merge_managed_hooks(
            &mut root,
            &managed_cmd("/app/claude-hooks/hook.sh"),
        );

        // Stop：用户 1 条在前 + 自有尾插。
        let stop = root["hooks"]["Stop"].as_array().unwrap();
        assert_eq!(stop.len(), 2);
        assert_eq!(stop[0]["hooks"][0]["command"], "echo user-stop");
        assert!(
            stop[1]["hooks"][0]["command"]
                .as_str()
                .unwrap()
                .contains("claude-hooks/hook.sh")
        );

        // PreToolUse（T4.1 已注册）：用户 matcher 条目 + 自有条目共存。
        let pre = root["hooks"]["PreToolUse"].as_array().unwrap();
        assert_eq!(pre.len(), 2);
        assert_eq!(pre[0]["matcher"], "Bash");
        assert!(
            pre[1]["hooks"][0]["command"]
                .as_str()
                .unwrap()
                .contains("claude-hooks/hook.sh")
        );

        // 8 注册事件全部在场；PermissionRequest 带 matcher "*"，SessionStart 无。
        assert_eq!(
            root["hooks"]["PermissionRequest"][0]["matcher"],
            "*"
        );
        assert!(
            root["hooks"]["SessionStart"][0]
                .get("matcher")
                .is_none()
        );
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
            assert!(
                root["hooks"].get(event).is_some(),
                "missing {event}"
            );
        }

        // 未注册事件键与顶层用户键保留。
        assert_eq!(
            root["hooks"]["CustomEvent"][0]["hooks"][0]["command"],
            "echo user-custom"
        );
        assert_eq!(root["permissions"], json!({"allow": ["Bash"]}));
    }

    #[test]
    fn merge_replaces_stale_managed_not_duplicates() {
        // 旧实例路径的自有条目（dev → release 切换）被 needle 扫掉并替换为新 command。
        let mut root = json!({
            "hooks": {
                "Stop": [{
                    "hooks": [{"type": "command", "command": managed_cmd("/old-dev-dir/claude-hooks/hook.sh")}]
                }]
            }
        });

        merge_managed_hooks(
            &mut root,
            &managed_cmd("/new-release-dir/claude-hooks/hook.sh"),
        );

        let stop = root["hooks"]["Stop"].as_array().unwrap();
        assert_eq!(stop.len(), 1);
        assert!(
            stop[0]["hooks"][0]["command"]
                .as_str()
                .unwrap()
                .contains("/new-release-dir/")
        );
    }

    #[test]
    fn strip_removes_managed_keeps_user_sibling_handler() {
        // 同一 definition 内用户 handler 与自有 handler 并存：只剥自有。
        let def = json!([{
            "hooks": [
                {"type": "command", "command": "echo user"},
                {"type": "command", "command": managed_cmd("/app/claude-hooks/hook.sh")}
            ]
        }]);
        let stripped = strip_managed_definitions(def.as_array().unwrap());
        assert_eq!(stripped.len(), 1);
        let handlers = stripped[0]["hooks"].as_array().unwrap();
        assert_eq!(handlers.len(), 1);
        assert_eq!(handlers[0]["command"], "echo user");
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

        // 外部改写（用户/claude 编辑）：内容真变化（加 permissions + 用户 hook）
        // 且格式全变（4 空格缩进 + 无尾换行）→ 语义比较判不等 → 写盘 + .bak 滚动。
        let external = r#"{
    "permissions": {
        "allow": ["WebSearch"]
    },
    "hooks": {
        "Stop": [
            {"matcher": "", "hooks": [{"type": "command", "command": "echo user-stop"}]}
        ]
    }
}"#;
        std::fs::write(&settings, external).unwrap();
        install(&ws, &base).unwrap();
        let second = std::fs::read_to_string(&settings).unwrap();
        assert!(second.contains("WebSearch"));
        assert!(second.contains("echo user-stop"));
        assert!(second.contains("claude-hooks/hook.sh"));
        assert!(
            settings.with_extension("json.bak").exists(),
            "backup rolled"
        );
        assert_eq!(
            std::fs::read_to_string(settings.with_extension("json.bak")).unwrap(),
            external,
            "backup holds the external version"
        );

        // 三次安装：语义相同跳过（文件不变，.bak 不滚仍是上一版）。
        install(&ws, &base).unwrap();
        assert_eq!(
            std::fs::read_to_string(&settings).unwrap(),
            second,
            "identical content skipped"
        );
        assert_eq!(
            std::fs::read_to_string(settings.with_extension("json.bak")).unwrap(),
            external,
            "backup not rolled on skip"
        );

        let _ = std::fs::remove_dir_all(&ws);
    }

    #[test]
    fn install_skips_when_only_format_changed() {
        // B 根治（T6.1）：外部（claude CLI）改写文件但语义等价——仅格式差异
        // （紧凑/无尾换行）→ 语义比较跳过：零写、.bak 不滚、文件保持外部格式。
        let ws = workspace_fixture();
        let base = ws.join("appdata");
        let settings = ws.join(".claude").join("settings.json");
        install(&ws, &base).unwrap();
        let ours = std::fs::read_to_string(&settings).unwrap();

        let doc: Value = serde_json::from_str(&ours).unwrap();
        // 变体 1：紧凑序列化 + 无尾换行。
        let compact = serde_json::to_string(&doc).unwrap();
        std::fs::write(&settings, &compact).unwrap();
        install(&ws, &base).unwrap();
        assert_eq!(
            std::fs::read_to_string(&settings).unwrap(),
            compact,
            "format-only change skipped (compact)"
        );
        // 变体 2：pretty 但无尾换行（对齐 claude JSON.stringify 形态）。
        let pretty_no_nl = serde_json::to_string_pretty(&doc).unwrap();
        std::fs::write(&settings, &pretty_no_nl).unwrap();
        install(&ws, &base).unwrap();
        assert_eq!(
            std::fs::read_to_string(&settings).unwrap(),
            pretty_no_nl,
            "format-only change skipped (pretty without trailing newline)"
        );
        assert!(
            !settings.with_extension("json.bak").exists(),
            "no backup rolled on format-only skips"
        );

        let _ = std::fs::remove_dir_all(&ws);
    }

    #[test]
    fn install_preserves_user_content_on_malformed_hooks_shapes() {
        // A 根治（T6.1）：hooks 子树语义损坏（合法 JSON 但形态意外）→ 用户内容
        // 原样保留，可识别部分正常安装，不丢弃任何东西。
        let ws = workspace_fixture();
        let base = ws.join("appdata");
        let settings = ws.join(".claude").join("settings.json");
        std::fs::write(
            &settings,
            r#"{
  "permissions": {"allow": ["Bash"]},
  "hooks": {
    "Stop": "not-an-array",
    "PreToolUse": [
      "not-an-object",
      {"hooks": [{"type": "command", "command": "echo user-pre"}]}
    ]
  }
}"#,
        )
        .unwrap();

        install(&ws, &base).unwrap();
        let doc = read_settings_value(&settings);

        // 顶层用户键与损坏形态原样保留（Stop 非数组 → 跳过该事件安装；
        // 数组内非对象元素透传）。
        assert_eq!(doc["permissions"], json!({"allow": ["Bash"]}));
        assert_eq!(doc["hooks"]["Stop"], json!("not-an-array"));
        let pre = doc["hooks"]["PreToolUse"].as_array().unwrap();
        assert_eq!(pre[0], json!("not-an-object"));
        assert_eq!(pre[1]["hooks"][0]["command"], "echo user-pre");
        // 自有条目尾插在可识别事件（用户元素在前）；Stop（非数组）未获得。
        assert_eq!(pre.len(), 3);
        assert!(
            pre[2]["hooks"][0]["command"]
                .as_str()
                .unwrap()
                .contains("claude-hooks/hook.sh")
        );
        // 其余事件正常安装。
        assert!(doc["hooks"]["SessionStart"].is_array());

        let _ = std::fs::remove_dir_all(&ws);
    }

    #[test]
    fn install_skips_entirely_when_hooks_not_an_object() {
        // hooks 整树非对象：透传不动 + warn → 合并零变化 → 语义等价跳过
        // （零写零备份，安装降级但不破坏）。
        let ws = workspace_fixture();
        let base = ws.join("appdata");
        let settings = ws.join(".claude").join("settings.json");
        let original = r#"{"permissions": {"allow": ["Bash"]}, "hooks": "broken"}"#;
        std::fs::write(&settings, original).unwrap();

        install(&ws, &base).unwrap();
        assert_eq!(
            std::fs::read_to_string(&settings).unwrap(),
            original,
            "non-object hooks subtree left untouched"
        );
        assert!(
            !settings.with_extension("json.bak").exists(),
            "no backup on semantic no-op"
        );

        let _ = std::fs::remove_dir_all(&ws);
    }

    #[test]
    fn install_skips_entirely_when_settings_not_an_object() {
        // 合法 JSON 但根非对象：整体透传不动 + warn + 跳过安装。
        let ws = workspace_fixture();
        let base = ws.join("appdata");
        let settings = ws.join(".claude").join("settings.json");
        std::fs::write(&settings, "[1, 2]").unwrap();

        install(&ws, &base).unwrap();
        assert_eq!(
            std::fs::read_to_string(&settings).unwrap(),
            "[1, 2]"
        );
        assert!(!settings.with_extension("json.bak").exists());

        let _ = std::fs::remove_dir_all(&ws);
    }

    #[test]
    fn install_recovers_from_corrupt_settings() {
        // 语法损坏（非法 JSON）：空起步 + .bak 兜底原文（T1.2 语义不变）。
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
