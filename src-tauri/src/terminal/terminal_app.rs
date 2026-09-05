// Terminal.app AppleScript 跳转。
//
// Terminal.app 只有 window → tab 两层（无 session 概念），匹配条件仅 tty。
// 命中后：set selected tab of w to t; set index of w to 1（把窗口拉到最前）。
//
// 冷/热启动语义与 iterm2.rs 对齐（见其文件头注释）：
//   - 热启动：窗口集合已稳定，activate + count of windows 两分支照旧，无竞态。
//   - 冷启动：launch → do script 自建窗口 → 以 front window 为所有权锚点清扫
//     非本脚本窗口（兜底 Terminal.app 启动自建的默认空白窗口）→ 最后才 activate。
//     锚点在 do script 之后取：do script 新建的窗口立即成为前台窗口。
// 未运行/未命中都以 error 终止（osascript 非零退出 → OsaScriptFailed → 前端
// toast），不做无谓拉起、不静默无反应。

use tauri::{AppHandle, Manager};

use crate::shared::app_config::{
    AppConfigState, DEFAULT_TERMINAL_POST_OPEN_COMMAND, TERMINAL_POST_OPEN_COMMAND_KEY,
    read_app_config_raw,
};
use crate::terminal::{NavErr, Target, run_osascript};

const SCRIPT_TEMPLATE: &str = r#"
-- 未运行直接报错：匹配注定失败，不做无谓拉起。
if not (application "Terminal" is running) then
    error "Terminal is not running"
end if

set targetTTY to {tty}
set didSelect to false

tell application "Terminal"
    repeat with w in windows
        repeat with t in tabs of w
            if tty of t is targetTTY then
                set selected tab of w to t
                set index of w to 1
                set didSelect to true
                exit repeat
            end if
        end repeat
        if didSelect then exit repeat
    end repeat

    -- 未命中显式报错（原先静默结束，用户点击后毫无反馈）。
    if not didSelect then error "session not found: " & targetTTY
end tell

-- 命中后才把应用带到前台：失败路径零副作用。
tell application "Terminal" to activate
"#;

/// open_directory 脚本骨架。热启动沿用 count 两分支；冷启动走
/// 「launch → do script 自建窗口 → 以 front window 为锚点清扫 → activate」。
const OPEN_SCRIPT_TEMPLATE: &str = r#"
-- 运行态预判先于一切状态变更（探测本身不拉起应用）。
set wasRunning to (application "Terminal" is running)

tell application "Terminal"
    if wasRunning then
        -- 热启动：窗口集合已稳定，count 两分支无竞态。
        activate
        if (count of windows) is 0 then
            do script "cd {escaped_dir}{cmd_suffix}"
        else
            do script "cd {escaped_dir}{cmd_suffix}" in front window
        end if
    else
        -- 冷启动（时序固定）：launch 拉起但不触发 open/reopen 前台语义 →
        -- do script 自建窗口（立即成为前台窗口，即所有权锚点）→ 清扫一切
        -- 非本脚本窗口（兜底 Terminal.app 启动自建的默认空白窗口）→ activate。
        -- 不用 count of windows 判断：它与自启窗口创建互有先后，存在竞态。
        launch
        do script "cd {escaped_dir}{cmd_suffix}"
        set myWinId to id of front window
        -- 先收集 id 再按 id 关闭：迭代中直接 close 会让窗口索引错位
        -- （specifier 迟绑定，关错/漏关窗口）。取值必须用 item i of——
        -- contents of 在 tell 块内对整数迭代项运行期报 -1728。
        set otherIds to id of every window
        repeat with i from 1 to count of otherIds
            set anId to item i of otherIds
            if anId is not myWinId then
                close (first window whose id is anId)
            end if
        end repeat
        activate
    end if
end tell
"#;

fn applescript_string(s: Option<&str>) -> String {
    match s {
        None => "missing value".to_string(),
        Some(v) => {
            let escaped = v.replace('\\', "\\\\").replace('"', "\\\"");
            format!("\"{}\"", escaped)
        }
    }
}

fn render_script(target: &Target<'_>) -> String {
    SCRIPT_TEMPLATE.replace("{tty}", &applescript_string(target.tty))
}

/// 渲染 open_directory 脚本。
fn render_open_script(escaped_dir: &str, cmd_suffix: &str) -> String {
    OPEN_SCRIPT_TEMPLATE
        .replace("{escaped_dir}", escaped_dir)
        .replace("{cmd_suffix}", cmd_suffix)
}

/// 执行 Terminal.app 跳转。osascript 失败时返回 NavErr::OsaScriptFailed。
/// tty 为 None 时直接返回 OsaScriptFailed（Terminal.app 仅靠 tty 匹配，无 fallback）。
pub fn focus_session(target: &Target<'_>) -> Result<(), NavErr> {
    if target.tty.is_none() {
        return Err(NavErr::OsaScriptFailed {
            stderr: "tty is required for Terminal.app navigation".to_string(),
        });
    }
    let script = render_script(target);
    run_osascript(&script)
}

/// 在 Terminal.app 中打开目录：热启动时有窗口则 `do script ... in front window`
/// （新建 Tab）、无窗口则 `do script ...`（新建窗口）；冷启动时自建窗口并清扫
/// Terminal.app 自启的空白窗口，均执行 cd 到指定目录。
pub fn open_directory(app: &AppHandle, dir: &str) -> Result<(), NavErr> {
    let escaped_dir = escape_dir_for_applescript(dir);

    // 读取「cd 后追加命令」配置（全局），缺失视为空串（仅 cd）。
    let post_open_cmd = read_app_config_raw(
        &app.state::<AppConfigState>(),
        TERMINAL_POST_OPEN_COMMAND_KEY,
    )
    .ok()
    .flatten()
    .unwrap_or_else(|| DEFAULT_TERMINAL_POST_OPEN_COMMAND.to_string());
    let cmd_suffix = super::build_cmd_suffix(&post_open_cmd);

    let script = render_open_script(&escaped_dir, &cmd_suffix);
    run_osascript(&script)
}

/// 将目录路径转义后嵌入 AppleScript 的 `do script "cd ..."` 语句。
/// 仅返回 shell 安全的路径部分（单引号包裹 + 转义），不含 `cd` 前缀，由调用方拼命令。
fn escape_dir_for_applescript(dir: &str) -> String {
    // Shell: 单引号包裹，内部 ' 替换为 '\''（结束引号 → 转义单引号 → 重新开引号）
    let shell_safe = dir.replace('\'', "'\\''");
    let quoted = format!("'{}'", shell_safe);
    // AppleScript 字符串上下文: \\ → 字面 \, \" → 字面 "
    quoted.replace('\\', "\\\\").replace('"', "\\\"")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn render_includes_tty() {
        let target = Target {
            tty: Some("/dev/ttys004"),
        };
        let script = render_script(&target);
        assert!(script.contains("\"/dev/ttys004\""));
        assert!(script.contains("set index of w to 1"));
        // 未运行必须先报错（不拉起应用），且未命中必须报错（不静默）。
        assert!(script.contains("if not (application \"Terminal\" is running)"));
        assert!(script.contains("session not found"));
        // activate 必须在遍历之后：失败路径零副作用。
        let repeat_pos = script.find("repeat with w in windows").unwrap();
        let activate_pos = script.find("to activate").unwrap();
        assert!(activate_pos > repeat_pos);
    }

    #[test]
    fn open_script_cold_start_sweeps() {
        let script = render_open_script("'/tmp/repo'", "");
        // 运行态预判必须先于 tell 块（先于一切状态变更）。
        let probe_pos = script
            .find("set wasRunning to (application \"Terminal\" is running)")
            .unwrap();
        let tell_pos = script
            .find("tell application \"Terminal\"")
            .unwrap();
        assert!(probe_pos < tell_pos);
        // 冷启动：launch + 所有权清扫，兜底 Terminal.app 自启空白窗口。
        assert!(script.contains("launch"));
        assert!(script.contains("close (first window whose id is anId)"));
        // 热启动两分支保持原语义：无窗口建窗口，有窗口在 front window 建 Tab。
        assert!(script.contains("do script \"cd '/tmp/repo'\" in front window"));
    }

    #[test]
    fn tty_none_returns_error() {
        let target = Target { tty: None };
        let err = focus_session(&target).unwrap_err();
        assert!(matches!(err, NavErr::OsaScriptFailed { .. }));
    }

    #[test]
    fn escapes_quotes() {
        let target = Target { tty: Some("a\"b") };
        let script = render_script(&target);
        assert!(script.contains("\"a\\\"b\""));
    }
}
