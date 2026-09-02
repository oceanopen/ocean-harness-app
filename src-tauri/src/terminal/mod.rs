// terminal 域：跳转到宿主终端对应会话。
//
// 子模块按终端类型拆分：
//   iterm2       —— iTerm2.app AppleScript（tty 精确匹配）
//   terminal_app —— Terminal.app AppleScript（tty 精确匹配）
//
// dispatch 按 host_app 选择对应实现；Unknown 直接返回 UnsupportedHostApp。
// 未来扩展 VSCode / IntelliJ 内嵌终端只需在 terminal/ 下加文件并在 dispatch 加分支。

pub mod iterm2;
pub mod terminal_app;

use serde::{Deserialize, Serialize};
use specta::Type;

use tauri::AppHandle;

use crate::shared::types::TerminalApp;

/// 跳转目标。仅靠 tty 精确匹配会话身份。
#[derive(Clone, Debug)]
pub struct Target<'a> {
    pub tty: Option<&'a str>,
}

/// 跳转失败原因。对应前端 navigation-failed toast 文案细分。
#[derive(Clone, Debug, Serialize, Deserialize, Type)]
#[serde(rename_all = "camelCase", tag = "kind")]
pub enum NavErr {
    /// 宿主终端未识别（如 VSCode 内嵌 / Wezterm 等）。
    UnsupportedHostApp,
    /// osascript 执行失败（exit code 非零）。
    OsaScriptFailed { stderr: String },
    /// ClaudeSessionStore 找不到对应 pid 的会话（可能刚过期）。
    SessionNotFound,
    /// 其他 IO 错误。
    Io { message: String },
}

impl From<std::io::Error> for NavErr {
    fn from(e: std::io::Error) -> Self {
        NavErr::Io {
            message: e.to_string(),
        }
    }
}

/// 按 host_app 分发到对应终端跳转实现。
/// Unknown 直接返回 UnsupportedHostApp（不尝试 osascript，避免误调）。
pub fn dispatch(host_app: TerminalApp, target: &Target<'_>) -> Result<(), NavErr> {
    match host_app {
        TerminalApp::ITerm2 => iterm2::focus_session(target),
        TerminalApp::Terminal => terminal_app::focus_session(target),
        // OceanHarness：本 app 嵌入终端的聚焦联动在后续模块接线（前端切到对应 issue
        // 终端页），当前暂不支持跳转——但监控列表不过滤（区分于 Unknown）。
        TerminalApp::IntelliJ | TerminalApp::OceanHarness | TerminalApp::Unknown => {
            Err(NavErr::UnsupportedHostApp)
        }
    }
}

/// 按终端类型分发到对应 open_directory 实现。
/// IntelliJ / Unknown 直接返回 UnsupportedHostApp。
pub fn open_directory_dispatch(
    app: &AppHandle,
    host_app: TerminalApp,
    dir: &str,
) -> Result<(), NavErr> {
    match host_app {
        TerminalApp::ITerm2 => iterm2::open_directory(app, dir),
        TerminalApp::Terminal => terminal_app::open_directory(app, dir),
        TerminalApp::IntelliJ | TerminalApp::OceanHarness | TerminalApp::Unknown => {
            Err(NavErr::UnsupportedHostApp)
        }
    }
}

/// 把自定义命令转义后嵌入 AppleScript 字符串字面量内部（不加外层引号）。
/// 只做 AppleScript 字符串转义（`\` → `\\`、`"` → `\"`），不碰空格 / `$` / `&&` 等
/// shell 元字符——命令原样交给交互 shell 执行。与各子模块的 `escape_dir_for_applescript`
/// 不同：dir 需要 shell-safe 的空格转义，自定义命令不需要（空格是参数分隔，有意义）。
fn escape_command_for_applescript(cmd: &str) -> String {
    cmd.replace('\\', "\\\\").replace('"', "\\\"")
}

/// 计算拼到 `cd {dir}` 之后的命令后缀：`raw_cmd` trim 后为空则返回空串（仅 cd）；
/// 非空则返回 ` && {转义后命令}`，使最终 shell 命令等价于 `cd {dir} && {cmd}`。
fn build_cmd_suffix(raw_cmd: &str) -> String {
    match raw_cmd.trim() {
        "" => String::new(),
        c => format!(" && {}", escape_command_for_applescript(c)),
    }
}
