// iTerm2 AppleScript 跳转。
//
// 仅靠 tty of s = targetTTY 匹配——tty 是会话身份的唯一可靠来源，
// cwd/projectName 等文本子串匹配会因 prompt/历史输出污染而误命中其他项目会话。
// 命中后：select window/tab/session，并把窗口拉到最前（set index of w to 1）。
// tty 为 None 时直接报错，与 terminal_app.rs 行为对齐。

use std::process::Command;

use tauri::{AppHandle, Manager};

use crate::shared::app_config::{
    AppConfigState, DEFAULT_ITERM2_SPLIT_DIRECTION, ITERM2_SPLIT_DIRECTION_KEY,
};
use crate::terminal::{NavErr, Target};

const SCRIPT_TEMPLATE: &str = r#"
on selectSession(theWindow, theTab, theSession)
    tell application "iTerm2"
        select theWindow
        select theTab
        select theSession
    end tell
end selectSession

tell application "iTerm2"
    activate
    set targetTTY to {tty}
    set didSelect to false

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
end tell
"#;

/// 把字符串字面量转成 AppleScript 字符串字面量（双引号包裹 + 转义）。
fn applescript_string(s: &str) -> String {
    let escaped = s.replace('\\', "\\\\").replace('"', "\\\"");
    format!("\"{}\"", escaped)
}

fn render_script(target: &Target<'_>) -> String {
    // focus_session 已守卫 None，此处 unwrap 安全。
    SCRIPT_TEMPLATE.replace("{tty}", &applescript_string(target.tty.unwrap()))
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
    let output = Command::new("osascript")
        .args(["-e", &script])
        .output()?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr).to_string();
        return Err(NavErr::OsaScriptFailed { stderr });
    }
    Ok(())
}

/// 在 iTerm2 中打开目录：有窗口则新建 Tab，无窗口则新建窗口，并 cd 到指定目录。
/// 分屏方向由 `iterm2_split_direction` 配置项控制：
///   horizontal = 上下分屏，vertical = 左右分屏，none = 不分屏。
pub fn open_directory(app: &AppHandle, dir: &str) -> Result<(), NavErr> {
    let escaped_dir = escape_dir_for_applescript(dir);

    // 读取分屏方向配置，缺失或非法值回退为默认（horizontal = 上下分屏）。
    let split_direction = app
        .state::<AppConfigState>()
        .0
        .lock()
        .ok()
        .and_then(|conn| {
            let mut stmt = conn
                .prepare("SELECT value FROM app_config WHERE key = ?1")
                .ok()?;
            stmt.query_row(rusqlite::params![ITERM2_SPLIT_DIRECTION_KEY], |row| {
                row.get::<_, String>(0)
            })
            .ok()
        })
        .unwrap_or_else(|| DEFAULT_ITERM2_SPLIT_DIRECTION.to_string());

    let script = match split_direction.as_str() {
        // 不分屏：仅 cd 到目录，不执行 split
        "none" => format!(
            r#"
tell application "iTerm2"
    activate
    if (count of windows) is 0 then
        set newWin to (create window with default profile)
        tell current session of newWin
            write text "cd {escaped_dir}"
        end tell
    else
        tell current window
            set newTab to (create tab with default profile)
            tell current session of newTab
                write text "cd {escaped_dir}"
            end tell
        end tell
    end if
end tell
"#,
            escaped_dir = escaped_dir,
        ),
        // 分屏模式：horizontal 或 vertical
        _ => {
            let split_cmd = match split_direction.as_str() {
                "vertical" => "split vertically",
                _ => "split horizontally",
            };
            format!(
                r#"
tell application "iTerm2"
    activate
    if (count of windows) is 0 then
        set newWin to (create window with default profile)
        tell current session of newWin
            write text "cd {escaped_dir}"
            set splitSess to ({split_cmd} with default profile)
            tell splitSess
                write text "cd {escaped_dir}"
            end tell
        end tell
    else
        tell current window
            set newTab to (create tab with default profile)
            tell current session of newTab
                write text "cd {escaped_dir}"
                set splitSess to ({split_cmd} with default profile)
                tell splitSess
                    write text "cd {escaped_dir}"
                end tell
            end tell
        end tell
    end if
end tell
"#,
                escaped_dir = escaped_dir,
                split_cmd = split_cmd,
            )
        }
    };
    let output = Command::new("osascript")
        .args(["-e", &script])
        .output()?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr).to_string();
        return Err(NavErr::OsaScriptFailed { stderr });
    }
    Ok(())
}

/// 将目录路径转义后嵌入 AppleScript 的 `write text "cd ..."` 语句。
/// 仅返回 shell 安全的路径部分，不含 `cd` 前缀，由调用方拼命令。
/// 空格用反斜杠转义（`my\ dir`），不使用单引号包裹，生成更自然的 cd 命令。
fn escape_dir_for_applescript(dir: &str) -> String {
    // Shell: 空格前加反斜杠，使 `cd my\ dir` 正确处理含空格路径
    let shell_safe = dir.replace(' ', "\\ ");
    // AppleScript 字符串上下文: \\ → 字面 \, \" → 字面 "
    shell_safe.replace('\\', "\\\\").replace('"', "\\\"")
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
    }

    #[test]
    fn tty_none_returns_error() {
        let target = Target { tty: None };
        let err = focus_session(&target).unwrap_err();
        assert!(matches!(err, NavErr::OsaScriptFailed { .. }));
    }

    #[test]
    fn escapes_quotes() {
        let target = Target {
            tty: Some("a\"b"),
        };
        let script = render_script(&target);
        assert!(script.contains("\"a\\\"b\""));
    }
}
