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

use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::Duration;

use tauri::{AppHandle, Emitter, Manager, WebviewUrl, WebviewWindowBuilder};

use crate::shared::app_config::{
    AppConfigState, PET_CLAUDE_SESSIONS_SUMMARY_VISIBLE_KEY, read_app_config_raw,
    write_app_config_raw,
};
use crate::shared::events::EVENT_PET_CLAUDE_SESSIONS_TASK_REFIT;
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
const PET_POSITION_DEBOUNCE_MS: u64 = 600;

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
    // Moved 防抖令牌：每次 Moved 置 true 取消上一个待保存任务；新任务 sleep 后 swap 检查。
    let cancel: Arc<AtomicBool> = Arc::new(AtomicBool::new(false));
    let app_for_move = app.clone();
    win.on_window_event(move |event| {
        match event {
            tauri::WindowEvent::Moved(_) => {
                cancel.store(true, Ordering::SeqCst);
                let token = cancel.clone();
                let app = app_for_move.clone();
                thread::spawn(move || {
                    thread::sleep(Duration::from_millis(PET_POSITION_DEBOUNCE_MS));
                    // 若防抖窗口内又来 Moved，本任务放弃，留给后到的任务落盘。
                    if token.swap(false, Ordering::SeqCst) {
                        return;
                    }
                    let Some(w) = app.get_webview_window("pet-claude-sessions-summary") else {
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
                    if let Some(state) = app.try_state::<AppConfigState>() {
                        let _ = write_app_config_raw(
                            &*state,
                            PET_CLAUDE_SESSIONS_SUMMARY_POSITION_KEY,
                            &raw,
                        );
                    }
                    // pet 停止移动后通知 pet_task 重新对齐到 pet 当前位置：拖拽中不刷新（防抖），
                    // 停下来一次性定位。前端 refit 监听 → fit → position_near_pet 按 pet 当前坐标重定位。
                    let _ = app.emit(EVENT_PET_CLAUDE_SESSIONS_TASK_REFIT, ());
                });
            }
            tauri::WindowEvent::CloseRequested { api, .. } => {
                api.prevent_close();
                let _ = w.hide();
            }
            _ => {}
        }
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
        let visible = w.is_visible().unwrap_or(false);
        if visible {
            let _ = w.hide();
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
    // pet 显隐变化联动 pet_claude_sessions_task：显示则按 count 裁决，隐藏则强制 hide。
    if now_visible {
        let _ = pet_claude_sessions_task::show_pet_claude_sessions_task_window(app.clone());
    } else {
        let _ = pet_claude_sessions_task::hide_pet_claude_sessions_task_window(app.clone());
    }
    // 落盘显隐偏好，启动时据此恢复，避免重启后丢失用户的隐藏选择。
    if let Some(state) = app.try_state::<AppConfigState>() {
        let val = if now_visible {
            YesNo::Yes
        } else {
            YesNo::No
        };
        let _ = write_app_config_raw(
            &*state,
            PET_CLAUDE_SESSIONS_SUMMARY_VISIBLE_KEY,
            val.as_str(),
        );
    }
    Ok(now_visible)
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
