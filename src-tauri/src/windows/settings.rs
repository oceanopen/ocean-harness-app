use tauri::{Emitter, LogicalPosition, LogicalSize, Manager, WebviewUrl, WebviewWindowBuilder};

use crate::shared::events::EVENT_SETTINGS_NAVIGATE;
use crate::shared::screen::{
    DEFAULT_SIZE, SETTINGS_RATIO, find_monitor_for_tray, ratio_size, work_area_center,
};

#[tauri::command]
#[specta::specta]
pub fn show_settings_window(
    app: tauri::AppHandle,
    navigate_to: Option<String>,
) -> Result<(), String> {
    // 按 tray.rect() 所在屏算尺寸；探测失败用 DEFAULT_SIZE 兜底，后续 set_position 也跳过。
    let monitor = find_monitor_for_tray(&app, "tray");
    let (width, height) = monitor
        .as_ref()
        .map(|m| ratio_size(m, SETTINGS_RATIO))
        .unwrap_or(DEFAULT_SIZE);

    // 窗口是否已存在：决定深链用事件（二次唤起，webview 已就绪）还是初始 URL（首开）。
    let settings_win_is_existing = app.get_webview_window("settings").is_some();

    let settings_win = match app.get_webview_window("settings") {
        Some(w) => {
            // 二次唤起：显式重置尺寸，避免窗口实例首次建好后跨分辨率屏固化。
            let _ = w.set_size(LogicalSize::new(width, height));
            w
        }
        None => {
            let product = app
                .config()
                .product_name
                .as_deref()
                .unwrap_or("We Claude Terminal");
            // 首开深链走初始 URL（settings.html#/<section>，HashRouter 直接消费）而非事件：
            // build() 返回时 webview 尚未加载、前端 listen 未注册，此时 emit_to 必然丢失。
            let url = match &navigate_to {
                Some(section) => format!("settings.html#/{section}").into(),
                None => "settings.html".into(),
            };
            let win = WebviewWindowBuilder::new(
                &app,
                "settings",
                WebviewUrl::App(url),
            )
            .title(format!("{product} - 系统设置"))
            .inner_size(width, height)
            // 默认在主屏居中；下方 set_position 修正到 tray 所在屏，探测失败保持主屏。
            .center()
            // 窗口不进任务栏与 Alt+Tab（Windows/Linux），macOS 上为 no-op（Dock 由 ActivationPolicy 控制）。
            .skip_taskbar(true)
            .build()
            .map_err(|e| e.to_string())?;

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

    // 新建和二次唤起都按 tray 所在屏的 work_area 居中；在 show 之前调用，无视觉跳跃。
    if let Some(m) = &monitor {
        let (x, y) = work_area_center(m, width, height);
        let _ = settings_win.set_position(LogicalPosition::new(x, y));
    }

    let _ = settings_win.show();
    let _ = settings_win.unminimize();
    let _ = settings_win.set_focus();

    // 二次唤起深链走事件（此时 webview 已就绪、listen 已注册）；首开深链已在上方写入初始 URL。
    // 无参不 emit，保留上次分区 hash——窗口 hide 不销毁，hash 天然存活。
    if let (true, Some(section)) = (settings_win_is_existing, navigate_to) {
        let _ = app.emit_to("settings", EVENT_SETTINGS_NAVIGATE, section);
    }

    Ok(())
}
