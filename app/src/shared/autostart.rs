use tauri::{AppHandle, Manager};
use tauri_plugin_autostart::ManagerExt;

use crate::shared::app_config::{AppConfigState, LAUNCH_AT_LOGIN_KEY, write_app_config_raw};

/*
 * 开机自启动：launch_at_login 配置（YesNo）与系统登录项的双向桥。
 *
 * - 保存同步：lib.rs 配置监听命中本 key 时调 sync 按值分派 enable/disable，失败仅 warn。
 * - 启动对齐：init 以系统登录项真实状态回写配置镜像（不 emit，此时无窗口）；用户在系统
 *   设置手动增删登录项后，下次启动 app 配置即随真实状态对齐，保存路径不会复活手动删除。
 */
pub fn init(app: &AppHandle) {
    let enabled = app.autolaunch().is_enabled().unwrap_or_else(|e| {
        log::warn!("[autostart] read login item status failed, fallback to disabled: {e}");
        false
    });
    let value = if enabled {
        "Y"
    } else {
        "N"
    };
    let state: tauri::State<AppConfigState> = app.state();
    if let Err(e) = write_app_config_raw(&state, LAUNCH_AT_LOGIN_KEY, value) {
        log::warn!("[autostart] write launch_at_login mirror failed: {e}");
    }
}

/*
 * 配置值 → 系统登录项显式分派。"Y"/"N" 之外的值不静默兜底；
 * 失败仅 warn，下次启动对齐自愈。
 */
pub fn sync(app: &AppHandle, value: &str) {
    let result = match value {
        "Y" => app.autolaunch().enable(),
        "N" => app.autolaunch().disable(),
        other => {
            log::warn!("[autostart] unexpected launch_at_login value {other:?}, skip sync");
            return;
        }
    };
    if let Err(e) = result {
        log::warn!("[autostart] sync login item failed: {e}");
    }
}
