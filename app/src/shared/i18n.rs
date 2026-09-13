use sys_locale::get_locale;
use tauri::{AppHandle, Manager};

use crate::shared::app_config::{AppConfigState, LANGUAGE_KEY, read_app_config_raw};

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum ResolvedLanguage {
    ZhCn,
    En,
}

fn detect_system_language() -> ResolvedLanguage {
    match get_locale() {
        Some(locale) if locale.to_lowercase().starts_with("zh") => ResolvedLanguage::ZhCn,
        _ => ResolvedLanguage::En,
    }
}

pub fn resolve(raw: Option<&str>) -> ResolvedLanguage {
    match raw {
        Some("zh-CN") => ResolvedLanguage::ZhCn,
        Some("en") => ResolvedLanguage::En,
        _ => detect_system_language(),
    }
}

/// 读取持久化语言配置解析为后端语言（托盘菜单 / pet 右键菜单共用）。
/// 无配置或读取失败时回退系统语言检测。
pub fn current_language(app: &AppHandle) -> ResolvedLanguage {
    let Some(state) = app.try_state::<AppConfigState>() else {
        return resolve(None);
    };
    let raw = read_app_config_raw(state.inner(), LANGUAGE_KEY).unwrap_or(None);
    resolve(raw.as_deref())
}

/// 后端文案仅覆盖托盘菜单与 pet 右键菜单（业务文案在前端 react-i18next）。
/// 加 key 时同步 refresh_menu_texts 与 setup 的菜单构建。
pub fn menu_text(lang: ResolvedLanguage, key: &str) -> &'static str {
    match (lang, key) {
        (ResolvedLanguage::ZhCn, "panel") => "控制台",
        (ResolvedLanguage::ZhCn, "settings") => "系统设置",
        (ResolvedLanguage::ZhCn, "pet-show") => "显示桌宠",
        (ResolvedLanguage::ZhCn, "pet-hide") => "隐藏桌宠",
        (ResolvedLanguage::ZhCn, "restart") => "重启",
        (ResolvedLanguage::ZhCn, "quit") => "退出",
        (ResolvedLanguage::En, "panel") => "Console",
        (ResolvedLanguage::En, "settings") => "Settings",
        (ResolvedLanguage::En, "pet-show") => "Show Pet",
        (ResolvedLanguage::En, "pet-hide") => "Hide Pet",
        (ResolvedLanguage::En, "restart") => "Restart",
        (ResolvedLanguage::En, "quit") => "Quit",
        _ => "",
    }
}
