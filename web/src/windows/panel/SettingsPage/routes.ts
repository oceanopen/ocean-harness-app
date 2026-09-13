import { MENU_PATHS } from '../routes';

// SettingsPage 分区路由 SSOT（设置页已并入 panel，作为 /settings 隐藏菜单页的内部分区层）。
// 两层各管一段：panel/routes.ts 持有 '/settings' 顶层前缀（pathToMenu 把 /settings/* 归到
// 'settings'），本文件持有分区段与绝对 path 换算，单向依赖、无重复字面量。
// 分区是纯前端概念（不再是后端事件 payload），无需 isMenuKey 守卫。

// 分区标识。
export type SettingsSection
  = | 'appConfig'
    | 'terminalConfig'
    | 'projectConfig'
    | 'serviceConfig'
    | 'userProfile'
    | 'about';

// 默认分区（/settings 本体与未知子段的回落目标）。
export const DEFAULT_SECTION: SettingsSection = 'appConfig';

// 分区 → 相对 path 段：供 SettingsPage 内 descendant <Routes> 声明。
// 必须是相对段——/settings/* splat 下的内层 Routes 只能看到剥掉前缀后的剩余路径，
// 绝对子路径（/appConfig）永远匹配不上。
export const SECTION_PATHS: Record<SettingsSection, string> = {
  appConfig: 'appConfig',
  terminalConfig: 'terminalConfig',
  projectConfig: 'projectConfig',
  serviceConfig: 'serviceConfig',
  userProfile: 'userProfile',
  about: 'about',
};

// 分区 → 绝对 path：组合 panel 顶层 SSOT 的 '/settings' 前缀（单源）。
export function sectionToPath(section: SettingsSection): string {
  return `${MENU_PATHS.settings}/${SECTION_PATHS[section]}`;
}

// pathname → 分区。startsWith 兜底防尾斜杠；/settings 本体与未知子段回落默认分区
// （未知子段另由 SettingsPage 内层 <Route path="*"> replace 归一）。
export function pathToSection(pathname: string): SettingsSection {
  for (const [section, seg] of Object.entries(SECTION_PATHS)) {
    const prefix = `${MENU_PATHS.settings}/${seg}`;
    if (pathname === prefix || pathname.startsWith(`${prefix}/`)) {
      return section as SettingsSection;
    }
  }
  return DEFAULT_SECTION;
}

// 分区菜单元数据：SettingsPage 渲染左侧分区菜单（icon 在组件本地补充）、
// PanelApp 顶栏渲染两级面包屑的第二级 label，共用此单源。
export const SECTION_MENUS: ReadonlyArray<{ key: SettingsSection; labelI18nKey: string }> = [
  { key: 'appConfig', labelI18nKey: 'settings:menu.appConfig' },
  { key: 'terminalConfig', labelI18nKey: 'settings:menu.terminalConfig' },
  { key: 'projectConfig', labelI18nKey: 'settings:menu.projectConfig' },
  { key: 'serviceConfig', labelI18nKey: 'settings:menu.serviceConfig' },
  { key: 'userProfile', labelI18nKey: 'settings:menu.userProfile' },
  { key: 'about', labelI18nKey: 'settings:menu.about' },
];
