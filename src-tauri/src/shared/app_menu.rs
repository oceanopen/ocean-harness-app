// macOS 应用菜单：复刻 tauri 默认菜单（tauri-2.11.2 src/menu/menu.rs 的 Menu::default），
// 唯一差异是 File/Window 子菜单里的 close_window 预定义项换成不带 accelerator 的普通
// MenuItem——预定义项自带 ⌘W key equivalent，而 macOS 菜单 key equivalent 优先于
// WKWebView keydown，panel 开发工作台的 ⌘W（文件查看模式关预览 tab、tabs 空时关窗）
// 会根本到不了前端。让位后 ⌘W 由各窗口前端 document keydown 全权接管；其余预定义项
// （App 菜单、Edit 复制粘贴、View 全屏、Window 最小化/缩放等）逐项保留，行为不变。
// 应用为 Accessory 策略（无可见菜单栏），本菜单实际只承担 key equivalent 职责。
//
// 仅 macOS 生效（mod.rs cfg 门控）：Windows/Linux 本就无应用菜单，不设置保持现状。

use tauri::menu::{AboutMetadata, Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::{AppHandle, Runtime};

/// 「Close Window」菜单项 id（lib.rs on_menu_event 分发用；File/Window 两个子菜单各挂
/// 一个独立实例、共用同一 id——muda 菜单项不可重复挂载到多个子菜单）。
pub const CLOSE_WINDOW_MENU_ID: &str = "close-window";

/// 构建应用菜单：与 tauri 默认菜单逐子菜单对齐（App/File/Edit/View/Window/Help），
/// close_window 换成无 accelerator 的 MenuItem（仍可通过菜单事件关窗，只是不再占用 ⌘W 按键）。
pub fn build<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<Menu<R>> {
    let pkg_info = app.package_info();
    let about_metadata = AboutMetadata {
        name: Some(pkg_info.name.clone()),
        version: Some(pkg_info.version.to_string()),
        copyright: app.config().bundle.copyright.clone(),
        authors: app
            .config()
            .bundle
            .publisher
            .clone()
            .map(|p| vec![p]),
        ..Default::default()
    };

    let app_submenu = Submenu::with_items(
        app,
        pkg_info.name.clone(),
        true,
        &[
            &PredefinedMenuItem::about(app, None, Some(about_metadata))?,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::services(app, None)?,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::hide(app, None)?,
            &PredefinedMenuItem::hide_others(app, None)?,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::quit(app, None)?,
        ],
    )?;

    let file_submenu = Submenu::with_items(
        app,
        "File",
        true,
        &[&MenuItem::with_id(
            app,
            CLOSE_WINDOW_MENU_ID,
            "Close Window",
            true,
            None::<&str>,
        )?],
    )?;

    let edit_submenu = Submenu::with_items(
        app,
        "Edit",
        true,
        &[
            &PredefinedMenuItem::undo(app, None)?,
            &PredefinedMenuItem::redo(app, None)?,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::cut(app, None)?,
            &PredefinedMenuItem::copy(app, None)?,
            &PredefinedMenuItem::paste(app, None)?,
            &PredefinedMenuItem::select_all(app, None)?,
        ],
    )?;

    let view_submenu = Submenu::with_items(
        app,
        "View",
        true,
        &[&PredefinedMenuItem::fullscreen(app, None)?],
    )?;

    let window_submenu = Submenu::with_items(
        app,
        "Window",
        true,
        &[
            &PredefinedMenuItem::minimize(app, None)?,
            &PredefinedMenuItem::maximize(app, None)?,
            &PredefinedMenuItem::separator(app)?,
            &MenuItem::with_id(
                app,
                CLOSE_WINDOW_MENU_ID,
                "Close Window",
                true,
                None::<&str>,
            )?,
        ],
    )?;

    let help_submenu = Submenu::with_items(app, "Help", true, &[])?;

    Menu::with_items(
        app,
        &[
            &app_submenu,
            &file_submenu,
            &edit_submenu,
            &view_submenu,
            &window_submenu,
            &help_submenu,
        ],
    )
}
