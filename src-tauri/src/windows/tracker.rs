use tauri::{Manager, WebviewUrl, WebviewWindowBuilder};

// tracker 窗口（工作空间/项目/Issue 三级管理）：主工作区，全屏最大化、进任务栏。
// 与 settings/panel 不同：不按 tray 屏比例开窗，而是 maximized 占满主屏工作区。
// 关闭转隐藏（复用 webview 实例，二次唤起秒开、保留 React 状态），与 settings 一致。
#[tauri::command]
#[specta::specta]
pub fn show_tracker_window(app: tauri::AppHandle) -> Result<(), String> {
    let tracker_win = match app.get_webview_window("tracker") {
        Some(w) => w, // 已存在则复用
        None => {
            let product = app
                .config()
                .product_name
                .as_deref()
                .unwrap_or("We Claude Terminal");
            let win = WebviewWindowBuilder::new(
                &app,
                "tracker",
                WebviewUrl::App("tracker.html".into()),
            )
            .title(format!("{product} - 工作台"))
            // 全屏 = 最大化：占满主屏工作区，保留标题栏、可恢复窗口化。
            .maximized(true)
            // 主工作窗口进任务栏/Dock（与 settings/panel 的 skip_taskbar(true) 区分）。
            .skip_taskbar(false)
            .build()
            .map_err(|e| e.to_string())?;

            let w = win.clone();
            win.on_window_event(move |event| {
                if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                    api.prevent_close(); // 关闭 = 隐藏
                    let _ = w.hide();
                }
            });

            win
        }
    };

    let _ = tracker_win.show();
    let _ = tracker_win.unminimize();
    let _ = tracker_win.set_focus();

    Ok(())
}
