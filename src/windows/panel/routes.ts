import type { MenuKey } from './commandPalette/types';

// panel 窗口路由 SSOT。
// 风格：全 query——path 仅承载顶层页面，子状态走查询参数（顺序无关、全可选、单一路由形态）。
// 子状态 query 键名（跨页统一，读写共用常量，避免散落魔法字符串拼错而静默破坏 URL↔store 同步）：
//   tracker:      wid=<workspaceId>            （项目选中态保留 store，不入 URL）
//   devWorkbench: pid=<projectId> & iid=<issueId>
export const TRACKER_WID_PARAM = 'wid';
export const DEV_PID_PARAM = 'pid';
export const DEV_IID_PARAM = 'iid';

// 默认页（index '/' 重定向目标；pathToMenu 未知路径回落）。
export const DEFAULT_MENU: MenuKey = 'claudeSessions';

// 菜单 → 基础 path 映射：同时供 PanelApp 的 <Route path> 声明与 pathToMenu 派生消费。
export const MENU_PATHS: Record<MenuKey, string> = {
  claudeSessions: '/claudeSessions',
  serverStatus: '/serverStatus',
  repositories: '/repositories',
  tracker: '/tracker',
  devWorkbench: '/devWorkbench',
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

// 解析查询参数为整数；缺失/空/非法（含小数）返回 null。
export function numParam(value: string | null): number | null {
  if (value == null || value === '') {
    return null;
  }
  const n = Number(value);
  return Number.isInteger(n) ? n : null;
}

// 解析查询参数为非空字符串 id（如 issue uuid）；缺失/空返回 null。
export function strParam(value: string | null): string | null {
  return value == null || value === '' ? null : value;
}
