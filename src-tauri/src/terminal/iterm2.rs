// iTerm2 AppleScript 跳转。
//
// 应用寻址一律用 bundle id（见 ITERM2_BUNDLE_ID），不用名字 "iTerm2"：
// 冷启动时按名字寻址需经 LaunchServices 解析并拉起应用，部分机器该名字注册
// 失效（bundle 实际叫 iTerm.app），解析失败 → 脚本字典加载不到 → 编译期
// -2741（类名无法绑定，如 create window 里的 window）或运行期 -1728。
// 按 id 寻址不依赖名字注册，冷启动实测可靠。
//
// 冷/热启动语义（执行时序固定，杜绝竞态）：
//   每个脚本以 `application id ... is running` 预判开头——该探测不拉起应用，
//   是唯一先于一切状态变更的运行态事实。
//   - 热启动：窗口集合已稳定，activate + count of windows 两分支照旧，无竞态。
//   - 冷启动：launch（不触发 open/reopen 前台语义）→ create window 拿所有权
//     锚点 → cd/分屏 → 清扫一切非本脚本窗口（兜底 iTerm2 启动过程自建的默认
//     空白窗口，即「初次打开多出一个 ~ 空白窗口」的根因）→ 最后才 activate。
//     所有权锚点是 create window 的返回值，不依赖 count of windows 与 iTerm2
//     自启窗口创建之间的先后，天然无竞态。
//
// focus_session 仅靠 tty of s = targetTTY 匹配——tty 是会话身份的唯一可靠来源，
// cwd/projectName 等文本子串匹配会因 prompt/历史输出污染而误命中其他项目会话。
// 命中后：select window/tab/session，并把窗口拉到最前（set index of w to 1）。
// 未运行/未命中都以 error 终止（osascript 非零退出 → OsaScriptFailed → 前端
// toast），不做无谓拉起、不静默无反应。
// tty 为 None 时直接报错，与 terminal_app.rs 行为对齐。

use tauri::{AppHandle, Manager};

use crate::shared::app_config::{
    AppConfigState, DEFAULT_ITERM2_SPLIT_DIRECTION, DEFAULT_TERMINAL_POST_OPEN_COMMAND,
    ITERM2_SPLIT_DIRECTION_KEY, TERMINAL_POST_OPEN_COMMAND_KEY, read_app_config_raw,
};
use crate::terminal::{NavErr, Target, run_osascript};

/// iTerm2 的 bundle id。AppleScript 按 id 寻址不依赖 LaunchServices 的名字注册，冷启动可靠。
const ITERM2_BUNDLE_ID: &str = "com.googlecode.iterm2";

/// focus_session 脚本模板。时序（固定，勿破坏）：
/// 1. is running 预判（探测不拉起应用）——未运行直接 error：此时目标 tty 的宿主
///    进程已随应用退出、匹配注定失败，拉起只会留下 iTerm2 自启空白窗口；
/// 2. tell 块内纯遍历匹配，不 activate——失败路径零副作用；
/// 3. 未命中 error（原先静默结束，用户点击后毫无反馈）；
/// 4. 命中后 select + set index，最后才 activate 把应用带到前台。
const SCRIPT_TEMPLATE: &str = r#"
-- 未运行直接报错：匹配注定失败，不做无谓拉起。
if not (application id "{bundle_id}" is running) then
    error "iTerm2 is not running"
end if

on selectSession(theWindow, theTab, theSession)
    tell application id "{bundle_id}"
        select theWindow
        select theTab
        select theSession
    end tell
end selectSession

set targetTTY to {tty}
set didSelect to false

tell application id "{bundle_id}"
    repeat with w in windows
        repeat with t in tabs of w
            repeat with s in sessions of t
                set ttyMatches to false
                try
                    if tty of s is targetTTY then set ttyMatches to true
                end try

                if ttyMatches then
                    my selectSession(w, t, s)
                    set index of w to 1
                    set didSelect to true
                    exit repeat
                end if
            end repeat
            if didSelect then exit repeat
        end repeat
        if didSelect then exit repeat
    end repeat

    -- 未命中显式报错（原先静默结束，用户点击后毫无反馈）。
    if not didSelect then error "session not found: " & targetTTY
end tell

-- 命中后才把应用带到前台：失败路径零副作用。
tell application id "{bundle_id}" to activate
"#;

/// open_directory 脚本骨架。热启动沿用 count 两分支；冷启动走「launch → 自建
/// 窗口拿所有权锚点 → 清扫非本脚本窗口 → activate」，见文件头注释。
/// {body_*} 为 `tell current session of ...` 内部语句块（cd，可选分屏 + 第二条 cd）。
const OPEN_SCRIPT_TEMPLATE: &str = r#"
-- 运行态预判先于一切状态变更（探测本身不拉起应用）。
set wasRunning to (application id "{bundle_id}" is running)

tell application id "{bundle_id}"
    if wasRunning then
        -- 热启动：窗口集合已稳定，count 两分支无竞态。
        activate
        if (count of windows) is 0 then
            set newWin to (create window with default profile)
            tell current session of newWin
{body_warm_new_window}
            end tell
        else
            tell current window
                set newTab to (create tab with default profile)
                tell current session of newTab
{body_warm_new_tab}
                end tell
            end tell
        end if
    else
        -- 冷启动（时序固定）：launch 拉起但不触发 open/reopen 前台语义 →
        -- create window 拿所有权锚点 → cd/分屏 → 清扫一切非本脚本窗口
        -- （兜底 iTerm2 启动自建的默认空白窗口）→ 最后才 activate。
        -- 不用 count of windows 判断：它与 iTerm2 自启窗口创建互有先后。
        launch
        set newWin to (create window with default profile)
        tell current session of newWin
{body_cold}
        end tell
        -- 先收集 id 再按 id 关闭：迭代中直接 close 会让窗口索引错位
        -- （specifier 迟绑定，关错/漏关窗口）。取值必须用 item i of——
        -- contents of 在 tell 块内对整数迭代项运行期报 -1728。
        set otherIds to id of every window
        repeat with i from 1 to count of otherIds
            set anId to item i of otherIds
            if anId is not id of newWin then
                close (first window whose id is anId)
            end if
        end repeat
        activate
    end if
end tell
"#;

/// 把字符串字面量转成 AppleScript 字符串字面量（双引号包裹 + 转义）。
fn applescript_string(s: &str) -> String {
    let escaped = s.replace('\\', "\\\\").replace('"', "\\\"");
    format!("\"{}\"", escaped)
}

fn render_script(target: &Target<'_>) -> String {
    // focus_session 已守卫 None，此处 unwrap 安全。
    SCRIPT_TEMPLATE
        .replace("{bundle_id}", ITERM2_BUNDLE_ID)
        .replace("{tty}", &applescript_string(target.tty.unwrap()))
}

/// 生成 `tell current session of ...` 内部语句块：写入 cd；split_cmd 非空时
/// 追加分屏并在新 session 写第二条 cd。indent 为每行完整缩进。
fn session_body(
    indent: &str,
    split_cmd: Option<&str>,
    escaped_dir: &str,
    cmd_suffix: &str,
) -> String {
    let mut lines = vec![format!(
        r#"{indent}write text "cd {escaped_dir}{cmd_suffix}""#
    )];
    if let Some(split) = split_cmd {
        lines.push(format!(
            "{indent}set splitSess to ({split} with default profile)"
        ));
        lines.push(format!("{indent}tell splitSess"));
        lines.push(format!(
            r#"{indent}    write text "cd {escaped_dir}{cmd_suffix}""#
        ));
        lines.push(format!("{indent}end tell"));
    }
    lines.join("\n")
}

/// 渲染 open_directory 脚本。split_direction 语义与原实现一致：
/// none → 不分屏；vertical → 左右分屏；其余（含默认 horizontal）→ 上下分屏。
fn render_open_script(split_direction: &str, escaped_dir: &str, cmd_suffix: &str) -> String {
    let split_cmd = if split_direction == "none" {
        None
    } else {
        Some(match split_direction {
            "vertical" => "split vertically",
            _ => "split horizontally",
        })
    };
    OPEN_SCRIPT_TEMPLATE
        .replace("{bundle_id}", ITERM2_BUNDLE_ID)
        .replace(
            "{body_warm_new_window}",
            &session_body(
                "                ",
                split_cmd,
                escaped_dir,
                cmd_suffix,
            ),
        )
        .replace(
            "{body_warm_new_tab}",
            &session_body(
                "                    ",
                split_cmd,
                escaped_dir,
                cmd_suffix,
            ),
        )
        .replace(
            "{body_cold}",
            &session_body(
                "                ",
                split_cmd,
                escaped_dir,
                cmd_suffix,
            ),
        )
}

/// 执行 iTerm2 跳转。
/// - tty 为 None：返回 NavErr::OsaScriptFailed（iTerm2 仅靠 tty 匹配，无 fallback）。
/// - osascript 退出码非 0：返回 NavErr::OsaScriptFailed（含 stderr）。
pub fn focus_session(target: &Target<'_>) -> Result<(), NavErr> {
    if target.tty.is_none() {
        return Err(NavErr::OsaScriptFailed {
            stderr: "tty is required for iTerm2 navigation".to_string(),
        });
    }
    let script = render_script(target);
    run_osascript(&script)
}

/// 在 iTerm2 中打开目录：热启动时有窗口则新建 Tab、无窗口则新建窗口；冷启动时
/// 自建窗口并清扫 iTerm2 自启的空白窗口，均 cd 到指定目录。
/// 分屏方向由 `iterm2_split_direction` 配置项控制：
///   horizontal = 上下分屏，vertical = 左右分屏，none = 不分屏。
pub fn open_directory(app: &AppHandle, dir: &str) -> Result<(), NavErr> {
    let escaped_dir = escape_dir_for_applescript(dir);
    let state = app.state::<AppConfigState>();

    // 读取分屏方向配置，缺失或非法值回退为默认（horizontal = 上下分屏）。
    let split_direction = read_app_config_raw(&state, ITERM2_SPLIT_DIRECTION_KEY)
        .ok()
        .flatten()
        .unwrap_or_else(|| DEFAULT_ITERM2_SPLIT_DIRECTION.to_string());

    // 读取「cd 后追加命令」配置（全局），缺失视为空串（仅 cd）。
    // 拼成 ` && {cmd}` 后缀，使 write text 最终发出 `cd {dir} && {cmd}`。
    let post_open_cmd = read_app_config_raw(&state, TERMINAL_POST_OPEN_COMMAND_KEY)
        .ok()
        .flatten()
        .unwrap_or_else(|| DEFAULT_TERMINAL_POST_OPEN_COMMAND.to_string());
    let cmd_suffix = super::build_cmd_suffix(&post_open_cmd);

    let script = render_open_script(&split_direction, &escaped_dir, &cmd_suffix);
    run_osascript(&script)
}

/// 将目录路径转义后嵌入 AppleScript 的 `write text "cd ..."` 语句。
/// 仅返回 shell 安全的路径部分，不含 `cd` 前缀，由调用方拼命令。
/// 空格用反斜杠转义（`my\ dir`），不使用单引号包裹，生成更自然的 cd 命令。
fn escape_dir_for_applescript(dir: &str) -> String {
    // Shell: 空格前加反斜杠，使 `cd my\ dir` 正确处理含空格路径
    let shell_safe = dir.replace(' ', "\\ ");
    // AppleScript 字符串上下文: \\ → 字面 \, \" → 字面 "
    shell_safe
        .replace('\\', "\\\\")
        .replace('"', "\\\"")
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
        assert!(script.contains("selectSession"));
        assert!(script.contains("set index of w to 1"));
        // 回归守卫：必须按 bundle id 寻址；按名字寻址在冷启动时会因 LS 名字注册
        // 失效而编译失败（-2741）或解析失败（-1728）。
        assert!(script.contains(&format!(
            "tell application id \"{ITERM2_BUNDLE_ID}\""
        )));
        assert!(!script.contains("tell application \"iTerm2\""));
    }

    #[test]
    fn focus_script_guards_and_reports() {
        let target = Target {
            tty: Some("/dev/ttys004"),
        };
        let script = render_script(&target);
        // 未运行必须先报错（不拉起应用），且未命中必须报错（不静默）。
        assert!(script.contains(&format!(
            "if not (application id \"{ITERM2_BUNDLE_ID}\" is running)"
        )));
        assert!(script.contains("session not found"));
        // activate 必须在遍历之后：失败路径零副作用。
        let repeat_pos = script.find("repeat with w in windows").unwrap();
        let activate_pos = script.find("to activate").unwrap();
        assert!(activate_pos > repeat_pos);
    }

    #[test]
    fn open_script_cold_start_sweeps() {
        let script = render_open_script("horizontal", "/tmp/repo", "");
        // 运行态预判必须先于 tell 块（先于一切状态变更）。
        let probe_pos = script
            .find(&format!(
                "set wasRunning to (application id \"{ITERM2_BUNDLE_ID}\" is running)"
            ))
            .unwrap();
        let tell_pos = script.find("tell application id").unwrap();
        assert!(probe_pos < tell_pos);
        // 冷启动：launch + 所有权清扫，兜底 iTerm2 自启空白窗口。
        assert!(script.contains("launch"));
        assert!(script.contains("close (first window whose id is anId)"));
        // 默认 horizontal：三个分支（建窗口/建 Tab/冷启动）各写主 + 分屏 2 条 cd。
        assert!(script.contains("split horizontally"));
        assert_eq!(
            script
                .matches("write text \"cd /tmp/repo\"")
                .count(),
            6
        );
        // 热启动两分支保持原语义：无窗口建窗口，有窗口建 Tab。
        assert!(script.contains("create window with default profile"));
        assert!(script.contains("create tab with default profile"));
    }

    #[test]
    fn open_script_split_direction_variants() {
        let none = render_open_script("none", "/tmp/repo", "");
        assert!(!none.contains("split "));
        let vertical = render_open_script("vertical", "/tmp/repo", "");
        assert!(vertical.contains("split vertically"));
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
