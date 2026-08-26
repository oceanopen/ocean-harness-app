// 所有 Tauri 事件名的统一出口。SSOT 在 src-tauri/src/shared/events.rs（Rust 单源），
// 经 tauri-specta .constant() 导出到 bindings.ts，本文件仅 re-export 保持既有
// import 路径稳定。新增事件：events.rs 加 const + lib.rs 注册 .constant() 后
// pnpm gen:bindings，再在此补 re-export 行。

export { EVENT_APP_CONFIG_CHANGED } from './bindings';

export { EVENT_CLAUDE_SESSIONS_CHANGED } from './bindings';

export { EVENT_TRANSCRIPT_CHANGED } from './bindings';

export { EVENT_CLAUDE_SESSION_NAV_FAILED } from './bindings';

export { EVENT_PET_CLAUDE_SESSIONS_TASK_REFIT } from './bindings';

export { EVENT_PANEL_NAVIGATE } from './bindings';

export { EVENT_PANEL_SHOWN } from './bindings';

// settings 窗口导航请求（payload = 分区 MenuKey 字符串）。show_settings_window 在 show 后
// emit_to settings 窗口，SettingsApp 监听后切到指定分区（前端宽容解析，非法回落 appConfig）。
export { EVENT_SETTINGS_NAVIGATE } from './bindings';

export { EVENT_HTTP_SERVER_STATE_CHANGED } from './bindings';

// claude runtime 状态变更（T2.1）：hook 事件经 Rust 归一化后 emit，
// useClaudeRuntime 按 payload.pane 过滤订阅。
export { EVENT_CLAUDE_RUNTIME_CHANGED } from './bindings';
