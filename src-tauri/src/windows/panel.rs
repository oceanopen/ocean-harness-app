// panel 窗口（控制台）+ 桥接命令。
//
// 会话扫描 / watcher / poller 逻辑全部下沉到 sessions/ 域（见 crate::sessions）。
// 终端跳转逻辑全部下沉到 terminal/ 域（见 crate::terminal）。
// 本文件仅负责：窗口创建、命令包装（get_claude_sessions / navigate_to_claude_session）。

use tauri::{
    AppHandle, Emitter, LogicalPosition, Manager, State, WebviewUrl, WebviewWindowBuilder,
};

use crate::shared::events::EVENT_CLAUDE_SESSION_NAV_FAILED;
use crate::shared::events::EVENT_PANEL_NAVIGATE;
use crate::shared::events::EVENT_PANEL_SHOWN;
use crate::shared::screen::{
    DEFAULT_SIZE, PANEL_RATIO, find_monitor_for_tray, ratio_size, work_area_center,
};
use crate::shared::state::claude_sessions::ClaudeSessionStore;
use crate::shared::types::ClaudeSessionInfo;
#[cfg(target_os = "macos")]
use crate::terminal::open_directory_dispatch;
use crate::terminal::{NavErr, Target, dispatch};

#[tauri::command]
#[specta::specta]
pub fn get_claude_sessions(
    state: State<'_, ClaudeSessionStore>,
) -> Result<Vec<ClaudeSessionInfo>, String> {
    let map = state.0.lock().map_err(|e| e.to_string())?;
    Ok(map.values().cloned().collect())
}

/// 手动刷新会话列表：触发全量重扫并广播 claude-sessions:changed，
/// 订阅该事件的前端（panel 的 ClaudeSessionsPage 页、pet_claude_sessions_summary、pet_claude_sessions_task）自动收到新快照。
/// force_git=true：手动刷新强制重算空闲会话的 GitPending，立即反映最新 git 状态。
#[tauri::command]
#[specta::specta]
pub fn refresh_sessions(app: AppHandle) -> Result<(), String> {
    crate::sessions::rescan(&app, true);
    Ok(())
}

/// 跳转到 pid 对应的宿主终端会话。
/// 成功返回 Ok(())；失败 emit `claude-sessions:nav-failed` 事件，
/// 前端据 NavErr.kind 渲染差异化 toast。
#[tauri::command]
#[specta::specta]
pub fn navigate_to_claude_session(pid: u32, app: AppHandle) -> Result<(), String> {
    // ClaudeSessionStore 查找：刚 expire 的会话走 SessionNotFound 路径，前端提示重试。
    let session = {
        let store = app.state::<ClaudeSessionStore>();
        let map = store.0.lock().map_err(|e| e.to_string())?;
        map.get(&pid.to_string()).cloned()
    };

    let Some(session) = session else {
        let _ = app.emit(
            EVENT_CLAUDE_SESSION_NAV_FAILED,
            &NavErr::SessionNotFound,
        );
        return Ok(()); // emit 后视作"已通知前端"，命令本身不算失败。
    };

    // 构造 Target：仅 tty 用于跳转匹配（iTerm2 / Terminal.app 均只靠 tty）。
    let target = Target {
        tty: if session.tty.is_empty() {
            None
        } else {
            Some(&session.tty)
        },
    };

    if let Err(err) = dispatch(session.host_app, &target) {
        log::warn!(
            "[panel] navigate_to_claude_session pid={} host={:?} failed: {:?}",
            pid,
            session.host_app,
            err
        );
        let _ = app.emit(EVENT_CLAUDE_SESSION_NAV_FAILED, &err);
    }
    Ok(())
}

/// 用指定编辑器打开项目目录。
///
/// **macOS**：GUI 应用从 Dock/Finder 启动时继承的 PATH 仅 `/usr/bin:/bin:/usr/sbin:/sbin`，
/// 不含 `/usr/local/bin`——直接 spawn `code` / `idea` CLI 会 ENOENT（用户在终端手动执行
/// `code` 能用是因为 shell 读 ~/.zshrc 补全了 PATH；从 .app bundle 启动则不走 shell 流程）。
/// 改用 macOS 原生 `open -a <App>`：`open` 在 /usr/bin 不依赖 PATH，由 LaunchServices 解析
/// 应用。IDEA 应用名因版本而异（CE/Ultimate/EDU），依次尝试候选名，首个成功即返回。
///
/// **Windows / Linux**：GUI 应用继承的 PATH 通常含编辑器安装目录（VSCode 安装时默认勾选
/// Add to PATH，IDEA 由 JetBrains Toolbox 创建 shell link），直接走 `code` / `idea` CLI 即可。
///
/// editor 仅允许 "vscode" / "idea"；应用未安装 / 启动失败返回 Err(String)，前端 warn。
#[tauri::command]
#[specta::specta]
pub fn open_in_editor(editor: String, cwd: String) -> Result<(), String> {
    // GUI 编辑器（尤其 IDEA）会把 Kotlin/Maven/Gradle 日志写到继承的 stdout/stderr，
    // 污染 we-claude-terminal 终端，stdio null 让子进程静默（所有平台共用）。
    #[cfg(target_os = "macos")]
    {
        let apps: &[&str] = match editor.as_str() {
            "vscode" => &["Visual Studio Code"],
            "idea" => &[
                "IntelliJ IDEA CE",
                "IntelliJ IDEA",
                "IntelliJ IDEA Ultimate",
                "IntelliJ IDEA EDU",
            ],
            other => {
                return Err(format!("unsupported editor: {other}"));
            }
        };
        for app in apps {
            // open 启动 GUI 应用后立即返回（不阻塞等应用退出）；应用不存在则 exit code 非零。
            let ok = std::process::Command::new("open")
                .args(["-a", app, &cwd])
                .stdin(std::process::Stdio::null())
                .stdout(std::process::Stdio::null())
                .stderr(std::process::Stdio::null())
                .status()
                .map(|s| s.success())
                .unwrap_or(false);
            if ok {
                return Ok(());
            }
        }
        Err(format!(
            "failed to launch {editor}: no matching app found"
        ))
    }

    #[cfg(not(target_os = "macos"))]
    {
        let cmd = match editor.as_str() {
            "vscode" => "code",
            "idea" => "idea",
            other => {
                return Err(format!("unsupported editor: {other}"));
            }
        };
        std::process::Command::new(cmd)
            .arg(&cwd)
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .map_err(|e| format!("failed to launch {cmd}: {e}"))?;
        Ok(())
    }
}

/// 判断 cwd 是否 Java 项目（Maven pom.xml 或 Gradle build.gradle/build.gradle.kts）。
/// 前端据此在非 Java 项目下禁用 IDEA（VSCode 在任何项目下都可用）。
#[tauri::command]
#[specta::specta]
pub fn is_java_project(cwd: String) -> bool {
    let path = std::path::Path::new(&cwd);
    path.join("pom.xml").exists()
        || path.join("build.gradle").exists()
        || path.join("build.gradle.kts").exists()
}

/// 用指定终端打开目录。仅 macOS 支持；terminal 仅允许 "iterm2" / "terminal"。
/// 有窗口则新建 Tab，无窗口则新建窗口，并 cd 到指定目录。
/// iTerm2 模式下根据 `iterm2_split_direction` 配置决定分屏方向（默认上下分屏）。
#[tauri::command]
#[specta::specta]
pub fn open_in_terminal(app: AppHandle, terminal: String, dir: String) -> Result<(), String> {
    #[cfg(target_os = "macos")]
    {
        use crate::shared::types::TerminalApp;
        let host_app = match terminal.as_str() {
            "iterm2" => TerminalApp::ITerm2,
            "terminal" => TerminalApp::Terminal,
            other => {
                return Err(format!("unsupported terminal: {other}"));
            }
        };
        open_directory_dispatch(&app, host_app, &dir).map_err(|e| format!("{e:?}"))
    }
    #[cfg(not(target_os = "macos"))]
    {
        let _ = (app, terminal, dir);
        Err("open_in_terminal is only supported on macOS".to_string())
    }
}

/// 用系统默认文件管理器打开目录：macOS Finder(open) / Windows Explorer(explorer) / Linux(xdg-open)。
/// 跨平台 cfg 与 stdio null 写法复刻 open_in_editor。explorer 退出码不可靠，用 spawn 不检查 status。
/// 从 shared/repositories.rs 迁移而来（仓库注册表已迁至 Go 服务，本命令作为通用 OS 动作保留）。
fn open_dir(dir: &str) -> Result<(), String> {
    #[cfg(target_os = "macos")]
    {
        std::process::Command::new("open")
            .arg(dir)
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .status()
            .map_err(|e| format!("failed to open directory: {e}"))?;
    }
    #[cfg(target_os = "windows")]
    {
        std::process::Command::new("explorer")
            .arg(dir)
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .map_err(|e| format!("failed to open directory: {e}"))?;
    }
    #[cfg(all(unix, not(target_os = "macos")))]
    {
        std::process::Command::new("xdg-open")
            .arg(dir)
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .status()
            .map_err(|e| format!("failed to open directory: {e}"))?;
    }
    Ok(())
}

/// 用系统文件管理器打开目录。dir 必须为存在的绝对路径。
#[tauri::command]
#[specta::specta]
pub fn open_in_file_manager(dir: String) -> Result<(), String> {
    let path = std::path::Path::new(&dir);
    if dir.is_empty() || !path.is_absolute() {
        return Err("invalid directory".into());
    }
    open_dir(&dir)
}

/// 用系统默认应用打开任意路径（文件或目录）：macOS 对文件走默认应用、目录走 Finder。
/// 终端链接点击（terminal_03 §3.4）的后端出口——与 open_in_file_manager 分立，
/// 语义各自明确（后者文档注释明确「目录」，跨平台行为也不一致：explorer 对文件
/// 是定位而非默认打开）。path 必须为非空绝对路径。
#[tauri::command]
#[specta::specta]
pub fn open_path(path: String) -> Result<(), String> {
    let p = std::path::Path::new(&path);
    if path.is_empty() || !p.is_absolute() {
        return Err("invalid path".into());
    }
    open_dir(&path)
}

#[tauri::command]
#[specta::specta]
pub fn show_panel_window(app: tauri::AppHandle, navigate_to: Option<String>) -> Result<(), String> {
    let monitor = find_monitor_for_tray(&app, "tray");
    let (width, height) = monitor
        .as_ref()
        .map(|m| ratio_size(m, PANEL_RATIO))
        .unwrap_or(DEFAULT_SIZE);

    let panel_win = match app.get_webview_window("panel") {
        // 窗口已存在（从隐藏恢复）：尊重用户上次的尺寸/位置/最大化状态，不强制重设。
        Some(w) => w,
        None => {
            let product = app
                .config()
                .product_name
                .as_deref()
                .unwrap_or("We Claude Terminal");
            let win = WebviewWindowBuilder::new(
                &app,
                "panel",
                WebviewUrl::App("panel.html".into()),
            )
            .title(format!("{product} - 控制台"))
            .inner_size(width, height) // 还原尺寸：取消最大化后回到此 85% 屏尺寸
            // 先隐藏：等下方定位 + 最大化完成后再由 panel_win.show() 显现，避免初帧跳动。
            .visible(false)
            .skip_taskbar(true)
            .build()
            .map_err(|e| e.to_string())?;

            // 必须在 maximize 之前定位：macOS 上对已最大化（zoom）态的窗口调用 set_position
            // 会破坏 zoom 定位、但保留撑开的全屏尺寸，导致「尺寸=全屏、左上角≠(0,0)」的错位窗口。
            // 先定还原坐标（托盘所在屏 work_area 居中，探测失败回退主屏居中），系统把它保存为还原帧。
            if let Some(m) = &monitor {
                let (x, y) = work_area_center(m, width, height);
                let _ = win.set_position(LogicalPosition::new(x, y));
            } else {
                let _ = win.center();
            }

            // 最后最大化：以工作区原点正确撑满；还原帧保留上面的居中坐标，取消最大化后回到 85% 居中。
            let _ = win.maximize();

            let w = win.clone();
            win.on_window_event(move |event| {
                if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                    api.prevent_close();
                    let _ = w.hide();
                }
            });

            win
        }
    };

    let _ = panel_win.show();
    let _ = panel_win.unminimize();
    let _ = panel_win.set_focus();

    // 先导航再通知 shown，确保前端收到 shown 时 activeMenu 已更新。
    if let Some(page) = navigate_to {
        let _ = app.emit_to("panel", EVENT_PANEL_NAVIGATE, page);
    }

    // 通知前端窗口已从隐藏恢复，订阅方（RepositoriesPage）据此触发数据刷新。
    let _ = app.emit_to("panel", EVENT_PANEL_SHOWN, ());

    Ok(())
}

#[cfg(test)]
mod tests {
    use std::path::Path;

    #[test]
    fn path_basename_fallback() {
        // 验证 Path::file_name 逻辑（enrich 也用同样模式取 project_name）。
        let name = Path::new("/Users/foo/proj")
            .file_name()
            .and_then(|s| s.to_str());
        assert_eq!(name, Some("proj"));
    }
}
