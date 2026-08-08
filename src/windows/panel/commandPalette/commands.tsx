import type { CommandConfig } from './types';
import {
  DashboardOutlined as DashboardOutlinedIcon,
  DeveloperModeOutlined as DeveloperModeOutlinedIcon,
  FolderOutlined as FolderOutlinedIcon,
  LanOutlined as LanOutlinedIcon,
  SensorsOutlined as SensorsOutlinedIcon,
  SettingsOutlined as SettingsOutlinedIcon,
  SpaceDashboardOutlined as SpaceDashboardOutlinedIcon,
  ViewSidebarOutlined as ViewSidebarOutlinedIcon,
  WorkspacesOutlined as WorkspacesOutlinedIcon,
} from '@mui/icons-material';

// 命令注册表数据源：v1 三组命令（导航 / 动作 / 跳转）。每条命令即数据——
// 不持有 UI，仅经 ctx 调用宿主能力或切换二级页面。新增命令在此数组追加即可，
// registry/Dialog 自动按 group 渲染与过滤，无需改动别处。
// icons 在模块级实例化（静态数组，渲染开销可忽略），避免每命令存组件类型再 render。
export const commands: CommandConfig[] = [
  // ── 导航：跳到四个顶级页面 ────────────────────────────────────────────
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
    id: 'nav.serverStatus',
    group: 'navigation',
    titleI18nKey: 'panel:commandPalette.nav.serverStatus',
    icon: <LanOutlinedIcon />,
    keywords: ['server', 'status', '服务'],
    action: ctx => ctx.navigate('serverStatus'),
    closeOnSelect: true,
  },
  {
    id: 'nav.tracker',
    group: 'navigation',
    titleI18nKey: 'panel:commandPalette.nav.tracker',
    icon: <SpaceDashboardOutlinedIcon />,
    keywords: ['tracker', 'workspace', 'projectIssue', 'kanban', '项目事项管理', '看板'],
    action: ctx => ctx.navigate('tracker'),
    closeOnSelect: true,
  },
  {
    id: 'nav.devWorkbench',
    group: 'navigation',
    titleI18nKey: 'panel:commandPalette.nav.devWorkbench',
    icon: <DeveloperModeOutlinedIcon />,
    keywords: ['devWorkbench', 'dev', 'workbench', '开发工作台', '开发'],
    action: ctx => ctx.navigate('devWorkbench'),
    closeOnSelect: true,
  },

  // ── 动作：全局快捷动作 ────────────────────────────────────────────────
  {
    id: 'action.settings',
    group: 'action',
    titleI18nKey: 'panel:commandPalette.action.settings',
    icon: <SettingsOutlinedIcon />,
    keywords: ['settings', 'preference', '设置', '偏好'],
    action: ctx => ctx.openSettings(),
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

  // ── 跳转：进入二级选择页，挑选实体后由宿主回写并跳转 ──────────────────
  {
    id: 'jump.workspace',
    group: 'jump',
    titleI18nKey: 'panel:commandPalette.jump.workspace',
    icon: <WorkspacesOutlinedIcon />,
    keywords: ['workspace', 'switch', '工作空间', '切换'],
    // 注释当前选中工作空间名（无则不显示），让用户在面板内即可看到当前上下文。
    getSubtitle: ctx => ctx.currentWorkspaceName,
    // 切二级页面而非直接关闭：选中后保留面板展示工作空间列表供二次过滤。
    action: ctx => ctx.setSubPage('workspace'),
    closeOnSelect: false,
  },
  {
    id: 'jump.project',
    group: 'jump',
    titleI18nKey: 'panel:commandPalette.jump.project',
    icon: <DashboardOutlinedIcon />,
    keywords: ['project', 'switch', '项目', '切换'],
    // 未选工作空间时软禁用（置灰 + 提示 + 回车无效），而非隐藏：项目列表依赖 workspaceId，
    // 但恒定渲染让命令列表长度稳定（避免 cmdk 选中态失序），并引导用户"先选工作空间"。
    isEnabled: ctx => ctx.currentWorkspaceId != null,
    disabledHintI18nKey: 'panel:commandPalette.jump.projectDisabledHint',
    getSubtitle: ctx => ctx.currentWorkspaceProjectName,
    action: ctx => ctx.setSubPage('project'),
    closeOnSelect: false,
  },
];
