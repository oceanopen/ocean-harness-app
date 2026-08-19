// settings 窗口路由 SSOT（对齐 panel/routes.ts 风格的裁剪版：无子状态 query，path 即全部路由形态）。
// MenuKey 同时是后端 EVENT_SETTINGS_NAVIGATE payload 集合（Rust 侧 Option<String> 不校验，由 isMenuKey 守卫）。

// settings 分区标识（原 SettingsApp 局部 type 迁入此处，供路由/侧栏/深链共用）。
export type MenuKey = 'appConfig' | 'terminalConfig' | 'projectConfig' | 'serviceConfig' | 'about';

// 默认分区（'/' 重定向目标；pathToMenu 未知路径回落；深链 payload 非法回落）。
export const DEFAULT_MENU: MenuKey = 'appConfig';

// 分区 → 基础 path 映射：同时供 SettingsApp 的 <Route path> 声明与 pathToMenu 派生消费。
export const MENU_PATHS: Record<MenuKey, string> = {
  appConfig: '/appConfig',
  terminalConfig: '/terminalConfig',
  projectConfig: '/projectConfig',
  serviceConfig: '/serviceConfig',
  about: '/about',
};

// 分区 → 基础 path（不含 query；settings 暂无子状态）。
export function menuToPath(menu: MenuKey): string {
  return MENU_PATHS[menu];
}

// pathname → 分区。startsWith 兜底防尾斜杠；未知路径回落默认分区。
export function pathToMenu(pathname: string): MenuKey {
  for (const [menu, prefix] of Object.entries(MENU_PATHS)) {
    if (pathname === prefix || pathname.startsWith(`${prefix}/`)) {
      return menu as MenuKey;
    }
  }
  return DEFAULT_MENU;
}

// 深链 payload 守卫：后端透传字符串，非法值由调用方回落 DEFAULT_MENU。
// hasOwn 防原型链穿透（in 会命中 toString/constructor 等）。
export function isMenuKey(value: unknown): value is MenuKey {
  return typeof value === 'string' && Object.hasOwn(MENU_PATHS, value);
}
