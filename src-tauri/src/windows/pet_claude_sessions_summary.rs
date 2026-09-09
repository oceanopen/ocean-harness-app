// 桌宠窗口：透明悬浮、置顶、无装饰、可拖拽。
//
//   transparent(true) + decorations(false) + always_on_top(true)
//   + skip_taskbar(true) + resizable(false) + shadow(false)
//
// 注：不做鼠标穿透——窗口的 128x128 矩形整体接收鼠标事件，牺牲矩形内
// 透明边角区域（会挡住下层）换取前端 mouseenter/cursor/拖拽的简单可靠。
//
// 初始位置：主屏右下角内缩 24px（避免被 Dock / 任务栏遮挡）。
// 尺寸：128x128 逻辑像素（足够展示 SVG 表情 + 状态徽章）。

use std::sync::mpsc;
use std::thread;
use std::time::Duration;

use tauri::{
    AppHandle, Emitter, LogicalPosition, Manager, WebviewUrl, WebviewWindowBuilder,
    menu::{ContextMenu, Menu, MenuItem},
};

use crate::shared::app_config::{
    AppConfigState, PET_CLAUDE_SESSIONS_SUMMARY_VISIBLE_KEY, read_app_config_raw,
    write_app_config_raw,
};
use crate::shared::events::EVENT_PET_CLAUDE_SESSIONS_TASK_REFIT;
use crate::shared::i18n::{current_language, menu_text};
use crate::shared::screen::{MonitorInfo, find_monitor_for_tray};
use crate::shared::types::YesNo;
use crate::windows::pet_claude_sessions_task;

/// 桌宠窗口尺寸（逻辑像素）。
const PET_SIZE: (f64, f64) = (128.0, 128.0);

/// 右下角内缩（逻辑像素），避开 Dock。
const PET_MARGIN: f64 = 24.0;

/// 持久化桌宠位置的 app_config key（逻辑坐标 JSON：`{"x":..,"y":..}`）。
const PET_CLAUDE_SESSIONS_SUMMARY_POSITION_KEY: &str = "pet_claude_sessions_summary_position";

/// Moved 防抖时长：拖动期间频繁触发，停顿后落盘一次。
/// 300ms：拖动结束到任务面板跟随的感知延迟上限，再长会显得「没跟上」。
const PET_POSITION_DEBOUNCE_MS: u64 = 300;

#[derive(serde::Serialize, serde::Deserialize)]
struct PetPositionSaved {
    x: f64,
    y: f64,
}

/// 计算桌宠初始位置（主屏右下角内缩 PET_MARGIN）。
/// 找不到 tray 所在屏时用 available_monitors 的第一块屏兜底；都失败返回 (100, 100)。
fn pet_position(app: &AppHandle) -> (f64, f64) {
    // 优先用上次保存的位置；缺失或损坏时回退主屏右下角。
    if let Some(state) = app.try_state::<AppConfigState>() {
        if let Ok(Some(raw)) =
            read_app_config_raw(&*state, PET_CLAUDE_SESSIONS_SUMMARY_POSITION_KEY)
        {
            if let Ok(saved) = serde_json::from_str::<PetPositionSaved>(&raw) {
                return (saved.x.max(0.0), saved.y.max(0.0));
            }
        }
    }

    let monitor = find_monitor_for_tray(app, "tray").or_else(|| {
        app.available_monitors()
            .ok()
            .and_then(|ms| ms.first().map(MonitorInfo::from_monitor))
    });
    let Some(m) = monitor else {
        return (100.0, 100.0);
    };
    let x = m.wa_x + m.wa_width - PET_SIZE.0 - PET_MARGIN;
    let y = m.wa_y + m.wa_height - PET_SIZE.1 - PET_MARGIN;
    (x.max(0.0), y.max(0.0))
}

/// 读取持久化的桌宠显隐偏好。缺失或非 "N" 均视为 true（向后兼容现有用户）。
fn pet_visible_pref(app: &AppHandle) -> bool {
    let Some(state) = app.try_state::<AppConfigState>() else {
        return true;
    };
    match read_app_config_raw(&*state, PET_CLAUDE_SESSIONS_SUMMARY_VISIBLE_KEY) {
        Ok(Some(v)) if v == YesNo::No.as_str() => false,
        _ => true,
    }
}

/// 创建或显示桌宠窗口。已存在则直接 show（不激活呈现，不夺 key window）。
pub fn ensure_pet_claude_sessions_summary_window(app: &AppHandle) -> tauri::Result<()> {
    if let Some(w) = app.get_webview_window("pet-claude-sessions-summary") {
        crate::shared::window_show::show_no_activate(&w);
        return Ok(());
    }

    let (x, y) = pet_position(app);
    let win = WebviewWindowBuilder::new(
        app,
        "pet-claude-sessions-summary",
        WebviewUrl::App("pet-claude-sessions-summary.html".into()),
    )
    .title("Pet")
    .inner_size(PET_SIZE.0, PET_SIZE.1)
    .position(x, y)
    // 关键透明/置顶属性
    .transparent(true)
    .decorations(false)
    .always_on_top(true)
    .skip_taskbar(true)
    .resizable(false)
    .shadow(false)
    .focused(false)
    .visible(false) // 先建后显，避免首屏白闪
    .accept_first_mouse(true) // macOS 未聚焦时首次点击即派发，配合前端 mousedown→hover
    .build()?;

    let w = win.clone();
    // Moved 防抖：单常驻 worker + mpsc 排水（尾沿防抖）。事件侧只 send（主线程零阻塞），
    // worker 静默满 PET_POSITION_DEBOUNCE_MS 才提交（落盘 + REFIT），最后一个事件天然由本次
    // 超时收尾——不存在「识别最后一个事件」的问题。
    // 注：替换了旧的「置位 + spawn + swap 检查」令牌方案——它置位发生在 spawn 之前，任务醒来
    // 无法区分位是自己的事件置的还是更新事件置的，最后一个 Moved 与前一个间隔超防抖时长时
    // （拖动末尾停顿后的单次微调）会被自己置的位误杀，落盘与 REFIT 均不执行，任务面板不跟随。
    let (moved_tx, moved_rx) = mpsc::channel::<()>();
    let app_for_move = app.clone();
    thread::spawn(move || {
        loop {
            // 静默期：阻塞等下一轮移动的第一个 Moved（窗口销毁/应用退出时 sender 落下即退出线程）。
            if moved_rx.recv().is_err() {
                return;
            }
            // 排水：每来一个事件重置计时，静默满防抖时长视为移动结束。
            while moved_rx
                .recv_timeout(Duration::from_millis(PET_POSITION_DEBOUNCE_MS))
                .is_ok()
            {}
            let Some(w) = app_for_move.get_webview_window("pet-claude-sessions-summary") else {
                return;
            };
            let Ok(scale) = w.scale_factor() else { return };
            let Ok(phys) = w.outer_position() else { return };
            let logical = phys.to_logical::<f64>(scale);
            let raw = serde_json::to_string(&PetPositionSaved {
                x: logical.x,
                y: logical.y,
            })
            .unwrap_or_default();
            if let Some(state) = app_for_move.try_state::<AppConfigState>() {
                let _ = write_app_config_raw(
                    &*state,
                    PET_CLAUDE_SESSIONS_SUMMARY_POSITION_KEY,
                    &raw,
                );
            }
            // pet 停止移动后通知 pet_task 重新对齐到 pet 当前位置：拖拽中不刷新（防抖），
            // 停下来一次性定位。前端 refit 监听 → fit → position_near_pet 按 pet 当前坐标重定位。
            let _ = app_for_move.emit_to(
                pet_claude_sessions_task::PET_CLAUDE_SESSIONS_TASK_LABEL,
                EVENT_PET_CLAUDE_SESSIONS_TASK_REFIT,
                (),
            );
        }
    });
    win.on_window_event(move |event| match event {
        tauri::WindowEvent::Moved(_) => {
            let _ = moved_tx.send(());
        }
        tauri::WindowEvent::CloseRequested { api, .. } => {
            api.prevent_close();
            let _ = w.hide();
        }
        _ => {}
    });

    // 首次 show 同样不激活（启动自启 / 设置开启场景不抢当前窗口焦点）。
    crate::shared::window_show::show_no_activate(&win);
    Ok(())
}

#[tauri::command]
#[specta::specta]
pub fn show_pet_claude_sessions_summary_window(app: AppHandle) -> Result<(), String> {
    ensure_pet_claude_sessions_summary_window(&app).map_err(|e| e.to_string())?;
    // pet 显示后联动评估 pet_claude_sessions_task 显隐（show_pet_claude_sessions_task_window 内部按 count 裁决），
    // 覆盖 pet 重新显示时前端 useEffect 因 count 未变不触发的边缘场景。
    pet_claude_sessions_task::show_pet_claude_sessions_task_window(app)
}

#[tauri::command]
#[specta::specta]
pub fn hide_pet_claude_sessions_summary_window(app: AppHandle) -> Result<(), String> {
    if let Some(w) = app.get_webview_window("pet-claude-sessions-summary") {
        let _ = w.hide();
    }
    // pet 隐藏联动隐藏 pet_claude_sessions_task，避免孤立的悬浮列表。
    pet_claude_sessions_task::hide_pet_claude_sessions_task_window(app)
}

#[tauri::command]
#[specta::specta]
pub fn toggle_pet_claude_sessions_summary_window(app: AppHandle) -> Result<bool, String> {
    let now_visible = if let Some(w) = app.get_webview_window("pet-claude-sessions-summary") {
        if w.is_visible().unwrap_or(false) {
            // 隐藏路径与 pet 右键菜单「隐藏桌宠」共用（隐藏 + 联动 pet_task + 落盘 + 刷托盘文案）。
            hide_and_persist(&app);
            false
        } else {
            // 托盘/设置开启桌宠：不激活呈现，不抢当前窗口焦点。
            crate::shared::window_show::show_no_activate(&w);
            true
        }
    } else {
        ensure_pet_claude_sessions_summary_window(&app).map_err(|e| e.to_string())?;
        true
    };
    // 显示路径：联动 pet_task 显隐（按 count 裁决）+ 落盘显隐偏好，启动时据此恢复。
    if now_visible {
        let _ = pet_claude_sessions_task::show_pet_claude_sessions_task_window(app.clone());
        if let Some(state) = app.try_state::<AppConfigState>() {
            let _ = write_app_config_raw(
                &*state,
                PET_CLAUDE_SESSIONS_SUMMARY_VISIBLE_KEY,
                YesNo::Yes.as_str(),
            );
        }
    }
    Ok(now_visible)
}

/// 隐藏桌宠：隐藏窗口 + 联动隐藏 pet_task + 落盘 No 偏好（重启后维持隐藏）+ 刷新托盘文案。
/// 托盘 toggle 的隐藏路径与 pet 右键菜单「隐藏桌宠」共用，保证两条入口语义一致。
pub fn hide_and_persist(app: &AppHandle) {
    if let Some(w) = app.get_webview_window("pet-claude-sessions-summary") {
        let _ = w.hide();
    }
    // pet 隐藏联动隐藏 pet_claude_sessions_task，避免孤立的悬浮列表。
    let _ = pet_claude_sessions_task::hide_pet_claude_sessions_task_window(app.clone());
    if let Some(state) = app.try_state::<AppConfigState>() {
        let _ = write_app_config_raw(
            &*state,
            PET_CLAUDE_SESSIONS_SUMMARY_VISIBLE_KEY,
            YesNo::No.as_str(),
        );
    }
    crate::windows::tray::refresh_menu_texts(app);
}

/// pet 右键菜单项 id。事件经 app 级 on_menu_event（lib.rs setup 注册）按 id 分发到
/// hide_and_persist；与托盘菜单 id（"pet-claude-sessions-summary" 等）空间独立，互不干扰。
pub const PET_CONTEXT_HIDE_MENU_ID: &str = "pet-context-hide";

/// 在桌宠窗口光标处弹原生右键菜单（前端 contextmenu 事件调用）。
/// 单项「隐藏桌宠」；原生菜单由系统渲染，不受 128x128 窗口边界裁切。
/// x/y 为前端传入的光标 CSS 逻辑坐标（相对窗口左上角），popup_at 内部按窗口 scale 换算。
#[tauri::command]
#[specta::specta]
pub fn show_pet_context_menu(app: AppHandle, x: f64, y: f64) -> Result<(), String> {
    let Some(win) = app.get_webview_window("pet-claude-sessions-summary") else {
        return Ok(());
    };
    let item = MenuItem::with_id(
        &app,
        PET_CONTEXT_HIDE_MENU_ID,
        menu_text(current_language(&app), "pet-hide"),
        true,
        None::<&str>,
    )
    .map_err(|e| e.to_string())?;
    let menu = Menu::with_items(&app, &[&item]).map_err(|e| e.to_string())?;
    // popup_at 接收 Window：经 Webview::window() 从 WebviewWindow 取宿主 Window。
    menu.popup_at(win.as_ref().window(), LogicalPosition::new(x, y))
        .map_err(|e| e.to_string())
}

/// 查询桌宠当前显隐状态。供前端启动时初始化 UI。
#[tauri::command]
#[specta::specta]
pub fn get_pet_claude_sessions_summary_visibility_state(app: AppHandle) -> bool {
    app.get_webview_window("pet-claude-sessions-summary")
        .and_then(|w| w.is_visible().ok())
        .unwrap_or(false)
}

/// 内部工具：app 启动时调用。读 pet_claude_sessions_summary_visible 偏好决定是否显示桌宠：
/// 用户上次选择隐藏（pet_claude_sessions_summary_visible = YesNo::No）时跳过窗口创建与 pet_claude_sessions_task 显示，
/// 维持隐藏态；否则确保桌宠可见并联动 pet_claude_sessions_task。
pub fn startup_show(app: &AppHandle) {
    if !pet_visible_pref(app) {
        return;
    }
    if let Err(e) = ensure_pet_claude_sessions_summary_window(app) {
        log::warn!(
            "[pet-claude-sessions-summary] startup ensure failed: {}",
            e
        );
    }
    // pet 显示后联动评估 pet_claude_sessions_task 显隐（show_pet_claude_sessions_task_window 内部按 count 裁决）。
    let _ = pet_claude_sessions_task::show_pet_claude_sessions_task_window(app.clone());
}
