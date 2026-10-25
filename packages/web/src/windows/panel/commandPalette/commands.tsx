import type { CommandConfig } from './types';
import {
  AssignmentOutlined as AssignmentOutlinedIcon,
  ExtensionOutlined as ExtensionOutlinedIcon,
  FolderOutlined as FolderOutlinedIcon,
  LanOutlined as LanOutlinedIcon,
  SensorsOutlined as SensorsOutlinedIcon,
  SettingsOutlined as SettingsOutlinedIcon,
  SmartToyOutlined as SmartToyOutlinedIcon,
  ViewSidebarOutlined as ViewSidebarOutlinedIcon,
  WorkspacesOutlined as WorkspacesOutlinedIcon,
} from '@mui/icons-material';

/**
 * 命令注册表数据源：v1 三组命令（导航 / 动作 / 跳转）。每条命令即数据——不持有 UI，仅经
 * ctx 调用宿主能力或切换二级页面，新增命令在此数组追加即可（registry/Dialog 自动按 group
 * 渲染与过滤）。icons 在模块级实例化（静态数组，渲染开销可忽略），避免每命令存组件类型再 render。
 */
export const commands: CommandConfig[] = [
  // ── 导航：跳到各顶级页面（隐藏菜单页经命令面板可达，与侧栏入口互补） ──────
  {
    id: 'nav.claudeSessions',
    group: 'navigation',
    titleI18nKey: 'panel:commandPalette.nav.claudeSessions',
    icon: <SensorsOutlinedIcon />,
    keywords: ['claude', 'session', '会话', '监听'],
    action: ctx => ctx.navigate('claudeSessions'),
    closeOnSelect: true,
  },
  {
    id: 'nav.repositories',
    group: 'navigation',
    titleI18nKey: 'panel:commandPalette.nav.repositories',
    icon: <FolderOutlinedIcon />,
    keywords: ['repository', 'repo', '仓库'],
    action: ctx => ctx.navigate('repositories'),
    closeOnSelect: true,
  },
  {
    id: 'nav.pluginMarketplace',
    group: 'navigation',
    titleI18nKey: 'panel:commandPalette.nav.pluginMarketplace',
    icon: <ExtensionOutlinedIcon />,
    keywords: ['plugin', 'marketplace', '插件', '市场'],
    action: ctx => ctx.navigate('pluginMarketplace'),
    closeOnSelect: true,
  },
  {
    id: 'nav.imBots',
    group: 'navigation',
    titleI18nKey: 'panel:commandPalette.nav.imBots',
    icon: <SmartToyOutlinedIcon />,
    keywords: ['im', 'bot', '机器人', '企微'],
    action: ctx => ctx.navigate('imBots'),
    closeOnSelect: true,
  },
  {
    id: 'nav.serverStatus',
    group: 'navigation',
    titleI18nKey: 'panel:commandPalette.nav.serverStatus',
    icon: <LanOutlinedIcon />,
    keywords: ['server', 'status', '服务'],
    action: ctx => ctx.navigate('serverStatus'),
    closeOnSelect: true,
  },
  {
    id: 'nav.workspaces',
    group: 'navigation',
    titleI18nKey: 'panel:commandPalette.nav.workspaces',
    icon: <WorkspacesOutlinedIcon />,
    keywords: ['workspace', 'workspaces', '工作空间'],
    action: ctx => ctx.navigate('workspaces'),
    closeOnSelect: true,
  },
  {
    id: 'nav.projects',
    group: 'navigation',
    titleI18nKey: 'panel:commandPalette.nav.projects',
    icon: <AssignmentOutlinedIcon />,
    keywords: ['project', 'task', '项目', '项目任务'],
    action: ctx => ctx.navigate('projects'),
    closeOnSelect: true,
  },
  // ── 动作：全局快捷动作 ────────────────────────────────────────────────
  {
    id: 'action.settings',
    group: 'action',
    titleI18nKey: 'panel:commandPalette.action.settings',
    icon: <SettingsOutlinedIcon />,
    keywords: ['settings', 'preference', '设置', '偏好'],
    action: ctx => ctx.navigate('settings'),
    closeOnSelect: true,
  },
  {
    id: 'action.toggleSidebar',
    group: 'action',
    titleI18nKey: 'panel:commandPalette.action.toggleSidebar',
    icon: <ViewSidebarOutlinedIcon />,
    keywords: ['sidebar', 'collapse', '侧栏', '折叠'],
    action: ctx => ctx.toggleSidebar(),
    closeOnSelect: true,
  },

];
