use std::sync::Mutex;
use tauri::{
    AppHandle, Emitter, Manager,
    menu::{Menu, MenuItem, PredefinedMenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
};

use crate::shared::app_config::{
    AppConfigState, LANGUAGE_KEY, PET_CLAUDE_SESSIONS_SUMMARY_DRAGGABLE_KEY, read_app_config_raw,
    write_app_config_raw,
};
use crate::shared::events::EVENT_APP_CONFIG_CHANGED;
use crate::shared::i18n::{ResolvedLanguage, menu_text, resolve};
use crate::shared::types::{AppConfigChangedPayload, YesNo};
use crate::windows::pet_claude_sessions_summary::get_pet_claude_sessions_summary_visibility_state;

/// 已构建的托盘菜单项引用，用于后续动态更新文案。
struct TrayMenuItems {
    panel: MenuItem<tauri::Wry>,
    settings: MenuItem<tauri::Wry>,
    pet_claude_sessions_summary: MenuItem<tauri::Wry>,
    drag: MenuItem<tauri::Wry>,
    restart: MenuItem<tauri::Wry>,
    quit: MenuItem<tauri::Wry>,
}

fn current_language(app: &AppHandle) -> ResolvedLanguage {
    let Some(state) = app.try_state::<AppConfigState>() else {
        return resolve(None);
    };
    let raw = read_app_config_raw(state.inner(), LANGUAGE_KEY).unwrap_or(None);
    resolve(raw.as_deref())
}

/// 桌宠当前显隐 → menu text key。隐藏时显示"显示桌宠"，显示时显示"隐藏桌宠"。
fn pet_claude_sessions_summary_menu_key(app: &AppHandle) -> &'static str {
    if get_pet_claude_sessions_summary_visibility_state(app.clone()) {
        "pet-hide"
    } else {
        "pet-show"
    }
}

/// 读取持久化的桌宠拖拽开关。缺失或非 "Y" 均视为关闭（默认不可拖拽）。
fn drag_enabled_pref(app: &AppHandle) -> bool {
    let Some(state) = app.try_state::<AppConfigState>() else {
        return false;
    };
    match read_app_config_raw(
        state.inner(),
        PET_CLAUDE_SESSIONS_SUMMARY_DRAGGABLE_KEY,
    ) {
        Ok(Some(v)) if v == YesNo::Yes.as_str() => true,
        _ => false,
    }
}

/// 拖拽开关当前态 → menu text key。已开启显示"关闭拖拽"，未开启显示"开启拖拽"
/// （动作指向目标态，与 pet-show/pet-hide 一致）。
fn drag_menu_key(app: &AppHandle) -> &'static str {
    if drag_enabled_pref(app) {
        "drag-off"
    } else {
        "drag-on"
    }
}

/// 查询当前 Control 键是否被按下。
///
/// Tauri 2.11.2 / tray-icon 0.23.1 的 TrayIconEvent::Click 不携带任何修饰键信息，
/// 因此托盘左键单击时无法从事件本身判断 Ctrl 状态，必须在触发瞬间主动查询键盘状态。
///
/// 平台说明：
/// - macOS：CGEventSourceFlagsState 读取硬件级修饰键状态（查询式 API，无需辅助功能权限）。
/// - Windows：GetAsyncKeyState 异步读取按键状态，最高位为 1 表示当前按下。
/// - Linux：Tauri 托盘 show_menu 本身不支持，返回 false，维持原左键行为。
fn control_key_pressed() -> bool {
    #[cfg(target_os = "macos")]
    {
        // kCGEventSourceStateHIDSystemState = 1（硬件级状态，最即时）；
        // kCGEventFlagMaskControl = 1 << 18 = 0x40000。
        #[link(name = "CoreGraphics", kind = "framework")]
        unsafe extern "C" {
            fn CGEventSourceFlagsState(state_id: i32, flags: u64) -> u64;
        }
        const STATE_HID: i32 = 1;
        const MASK_CONTROL: u64 = 1 << 18;
        unsafe { CGEventSourceFlagsState(STATE_HID, MASK_CONTROL) & MASK_CONTROL != 0 }
    }
    #[cfg(target_os = "windows")]
    {
        #[link(name = "user32")]
        unsafe extern "system" {
            fn GetAsyncKeyState(v_key: i32) -> i16;
        }
        const VK_CONTROL: i32 = 0x11;
        // 返回值最高位（bit 15）为 1 表示按键当前处于按下状态，i16 解读即为负数。
        unsafe { GetAsyncKeyState(VK_CONTROL) < 0 }
    }
    // Linux 等其他平台：托盘不支持编程式弹菜单，统一不识别 Ctrl。
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        false
    }
}

pub fn setup(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    // dev 模式用带 DEV 角标的图标，避免本地调试版与正式安装版在状态栏混淆。
    // include_bytes! 返回编译期固定大小数组 &[u8; N]，两套图标字节数不同，
    // 用 as_slice 统一为 &[u8]，cfg!(debug_assertions) 在编译期求值后编译器只保留对应分支。
    let icon_bytes: &[u8] = if cfg!(debug_assertions) {
        include_bytes!(concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/icons/32x32-dev.png"
        ))
        .as_slice()
    } else {
        include_bytes!(concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/icons/32x32.png"
        ))
        .as_slice()
    };
    let icon = tauri::image::Image::from_bytes(icon_bytes).expect("failed to load tray icon");

    let tooltip = app
        .config()
        .product_name
        .as_deref()
        .unwrap_or("We Claude Terminal");

    let lang = current_language(app.handle());

    let panel_item = MenuItem::with_id(
        app,
        "panel",
        menu_text(lang, "panel"),
        true,
        None::<&str>,
    )?;
    let settings_item = MenuItem::with_id(
        app,
        "settings",
        menu_text(lang, "settings"),
        true,
        None::<&str>,
    )?;
    let pet_claude_sessions_summary_item = MenuItem::with_id(
        app,
        "pet-claude-sessions-summary",
        menu_text(
            lang,
            pet_claude_sessions_summary_menu_key(app.handle()),
        ),
        true,
        None::<&str>,
    )?;
    // 拖拽开关：初始禁用，由首次 refresh_menu_texts 按桌宠显隐纠正。
    let drag_item = MenuItem::with_id(
        app,
        "drag",
        menu_text(lang, drag_menu_key(app.handle())),
        false,
        None::<&str>,
    )?;
    let quit_item = MenuItem::with_id(
        app,
        "quit",
        menu_text(lang, "quit"),
        true,
        None::<&str>,
    )?;
    let restart_item = MenuItem::with_id(
        app,
        "restart",
        menu_text(lang, "restart"),
        true,
        None::<&str>,
    )?;

    let menu = Menu::with_items(
        app,
        &[
            &panel_item,
            &PredefinedMenuItem::separator(app)?,
            &pet_claude_sessions_summary_item,
            &drag_item,
            &PredefinedMenuItem::separator(app)?,
            &settings_item,
            &restart_item,
            &quit_item,
        ],
    )?;

    TrayIconBuilder::with_id("tray")
        .icon(icon)
        .tooltip(tooltip)
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_tray_icon_event(|tray, event| {
            // 左键单击打开控制台窗口；右键由系统默认弹出菜单（无需处理）。
            // Ctrl+左键单击：与原生右键效果一致，弹出托盘菜单。click 事件本身不带修饰键，
            // 故在触发瞬间查询 Control 键状态：按下则走弹菜单分支，否则走打开控制台分支。
            // 注意：本回调首参是 &TrayIcon（非 &AppHandle），需经 app_handle() 取得句柄。
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                if control_key_pressed() {
                    // with_inner_tray_icon → show_menu 是弹菜单的唯一入口；其内部用
                    // run_on_main_thread + 同步阻塞等待结果。本回调可能在主线程触发，
                    // 直接调用会死锁。故 clone 后在独立后台线程触发：后台线程阻塞等待，
                    // 主线程不被阻塞即可正常派发 show_menu。
                    let tray = tray.clone();
                    std::thread::spawn(move || {
                        if let Err(e) = tray.with_inner_tray_icon(|inner| {
                            inner.show_menu();
                        }) {
                            log::warn!("failed to show tray menu: {e}");
                        }
                    });
                    return;
                }
                let app = tray.app_handle();
                if let Err(e) = crate::windows::panel::show_panel_window(app.clone(), None) {
                    log::warn!("failed to open panel window: {e}");
                }
            }
        })
        .on_menu_event(|app, event| match event.id().as_ref() {
            "panel" => {
                if let Err(e) = crate::windows::panel::show_panel_window(app.clone(), None) {
                    log::warn!("failed to open panel window: {e}");
                }
            }
            "pet-claude-sessions-summary" => {
                if let Err(e) = crate::windows::pet_claude_sessions_summary::toggle_pet_claude_sessions_summary_window(app.clone()) {
                    log::warn!("failed to toggle pet window: {e}");
                }
                // 切换后立刻刷新菜单文案（pet 文案 + drag enabled 都依赖显隐态）。
                crate::windows::tray::refresh_menu_texts(app);
            }
            "drag" => {
                // 翻转拖拽开关：落盘后广播 app-config-changed 通知前端 PetClaudeSessionsSummaryApp 实时响应，再刷新菜单文案。
                // drag 的 enabled 只随桌宠显隐变化，不由此处改变。
                let new_val = if drag_enabled_pref(app) {
                    YesNo::No
                } else {
                    YesNo::Yes
                };
                if let Some(state) = app.try_state::<AppConfigState>() {
                    let _ = write_app_config_raw(state.inner(), PET_CLAUDE_SESSIONS_SUMMARY_DRAGGABLE_KEY, new_val.as_str());
                }
                let _ = app.emit(
                    EVENT_APP_CONFIG_CHANGED,
                    AppConfigChangedPayload {
                        key: PET_CLAUDE_SESSIONS_SUMMARY_DRAGGABLE_KEY.to_string(),
                        value: new_val.as_str().to_string(),
                    },
                );
                crate::windows::tray::refresh_menu_texts(app);
            }
            "settings" => {
                // 托盘为通用入口：不传深链分区，保留上次分区（无深链事件）。
                if let Err(e) = crate::windows::settings::show_settings_window(app.clone(), None) {
                    log::warn!("failed to open settings window: {e}");
                }
            }
            "restart" => {
                app.restart();
            }
            "quit" => {
                app.exit(0);
            }
            _ => {}
        })
        .build(app)?;

    app.manage(Mutex::new(TrayMenuItems {
        panel: panel_item,
        settings: settings_item,
        pet_claude_sessions_summary: pet_claude_sessions_summary_item,
        drag: drag_item,
        restart: restart_item,
        quit: quit_item,
    }));

    Ok(())
}

pub fn refresh_menu_texts(app: &AppHandle) {
    let Some(state) = app.try_state::<Mutex<TrayMenuItems>>() else {
        return;
    };
    let Ok(items) = state.lock() else {
        return;
    };
    let lang = current_language(app);
    let _ = items.panel.set_text(menu_text(lang, "panel"));
    let _ = items
        .settings
        .set_text(menu_text(lang, "settings"));
    let _ = items
        .pet_claude_sessions_summary
        .set_text(menu_text(
            lang,
            pet_claude_sessions_summary_menu_key(app),
        ));
    // drag 文案随开关状态切换；enabled 随桌宠显隐（隐藏时禁用，避免无桌宠时操作）。
    let _ = items
        .drag
        .set_text(menu_text(lang, drag_menu_key(app)));
    let _ = items
        .drag
        .set_enabled(get_pet_claude_sessions_summary_visibility_state(
            app.clone(),
        ));
    let _ = items.restart.set_text(menu_text(lang, "restart"));
    let _ = items.quit.set_text(menu_text(lang, "quit"));
}
