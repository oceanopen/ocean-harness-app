import type { MenuKey } from './commandPalette/types';

/**
 * panel 窗口路由 SSOT。风格：全 query——path 仅承载顶层页面，子状态走查询参数（顺序无关、
 * 全可选、单一路由形态）；例外 settings 是唯一带嵌套子路径的页面（/settings/<section>，分区是子页面而非临时状态，分区段 SSOT 在 SettingsPage/routes.ts，pathToMenu 的 startsWith 匹配天然把 /settings/* 归到 'settings' 无需特判）。
 * 子状态 query 键名跨页统一（读写共用常量，避免散落魔法字符串拼错而静默破坏 URL↔store 同步）。
 */
// workspaces/projects 两页子状态：workspaces wid=<workspaceId> & cid=<conversationId>（会话
// 选中意图）；projects pid=<projectId> & tid=<taskId>（内容区恒由任务主会话编排，无 cid）。
export const WORKSPACES_WID_PARAM = 'wid';
export const WORKSPACES_CID_PARAM = 'cid';
export const PROJECTS_PID_PARAM = 'pid';
export const PROJECTS_TID_PARAM = 'tid';

// 默认页（index '/' 重定向目标；pathToMenu 未知路径回落）。
export const DEFAULT_MENU: MenuKey = 'claudeSessions';

// 菜单 → 基础 path 映射：同时供 PanelApp 的 <Route path> 声明与 pathToMenu 派生消费。
export const MENU_PATHS: Record<MenuKey, string> = {
  claudeSessions: '/claudeSessions',
  serverStatus: '/serverStatus',
  repositories: '/repositories',
  pluginMarketplace: '/pluginMarketplace',
  imBots: '/imBots',
  workspaces: '/workspaces',
  projects: '/projects',
  settings: '/settings',
};

// 菜单 → 基础 path（不含 query）。子状态页切回时优先用「记忆的上次完整路径」，回落到此。
export function menuToPath(menu: MenuKey): string {
  return MENU_PATHS[menu];
}

// pathname → 菜单。全 query 风格下 pathname 恒为基础 path；startsWith 兜底防尾斜杠。
export function pathToMenu(pathname: string): MenuKey {
  for (const [menu, prefix] of Object.entries(MENU_PATHS)) {
    if (pathname === prefix || pathname.startsWith(`${prefix}/`)) {
      return menu as MenuKey;
    }
  }
  return DEFAULT_MENU;
}

// 解析查询参数为非空字符串 id（如 workspace/issue uuid）；缺失/空返回 null。
export function strParam(value: string | null): string | null {
  return value == null || value === '' ? null : value;
}
