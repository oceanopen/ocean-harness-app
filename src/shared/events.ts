// 所有 Tauri 事件名的 SSOT。修改时必须同步 src-tauri/src/shared/events.rs（后端镜像）。
// 与后端 const EVENT_XXX 一一对应；specta 不自动导出 const &str，走双份维护。

export const EVENT_APP_CONFIG_CHANGED = 'app-config-changed';

export const EVENT_CLAUDE_SESSIONS_CHANGED = 'claude-sessions:changed';

export const EVENT_CLAUDE_SESSION_NAV_FAILED = 'claude-sessions:nav-failed';

export const EVENT_PET_CLAUDE_SESSIONS_TASK_REFIT = 'pet-claude-sessions-task:refit';

export const EVENT_PANEL_NAVIGATE = 'panel:navigate';

export const EVENT_PANEL_SHOWN = 'panel:shown';

// settings 窗口导航请求（payload = 分区 MenuKey 字符串）。show_settings_window 在 show 后
// emit_to settings 窗口，SettingsApp 监听后切到指定分区（前端宽容解析，非法回落 appConfig）。
export const EVENT_SETTINGS_NAVIGATE = 'settings:navigate';

export const EVENT_HTTP_SERVER_STATE_CHANGED = 'http-server:state-changed';
