import type { WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import type { MenuKey } from './commandPalette/types';
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import DeveloperModeOutlinedIcon from '@mui/icons-material/DeveloperModeOutlined';
import FolderOutlinedIcon from '@mui/icons-material/FolderOutlined';
import LanOutlinedIcon from '@mui/icons-material/LanOutlined';
import SensorsOutlinedIcon from '@mui/icons-material/SensorsOutlined';
import SettingsOutlinedIcon from '@mui/icons-material/SettingsOutlined';
import SpaceDashboardOutlinedIcon from '@mui/icons-material/SpaceDashboardOutlined';
import {
  alpha,
  Box,
  Breadcrumbs,
  IconButton,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Tooltip,
  Typography,
  useTheme,
} from '@mui/material';
import appIcon from '@src/assets/app-icon.svg';
import {
  DEFAULT_PANEL_SIDEBAR_COLLAPSED,
  isYes,
  PANEL_SIDEBAR_COLLAPSED_KEY,
  parseYesNo,
  setAppConfig,
  toYesNo,
} from '@src/shared/appConfig';
import { commands } from '@src/shared/bindings';
import { EVENT_PANEL_NAVIGATE, EVENT_PANEL_SHOWN } from '@src/shared/events';
import { useConfigValue } from '@src/shared/useConfigValue';
import { useTrackerStore } from '@src/state/tracker';
import { listen } from '@tauri-apps/api/event';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useLocation, useMatch, useNavigate } from 'react-router-dom';
import ClaudeSessionsPage from './ClaudeSessionsPage/ClaudeSessionsPage';
import CommandPaletteProvider from './commandPalette/CommandPaletteProvider';
import CommandPaletteTrigger from './commandPalette/CommandPaletteTrigger';
import DevWorkbenchPage from './DevWorkbenchPage/DevWorkbenchPage';
import RepositoriesPage from './RepositoriesPage/RepositoriesPage';
import { DEFAULT_MENU, menuToPath, pathToMenu, TRACKER_WID_PARAM } from './routes';
import ServerStatusIndicator from './ServerStatusIndicator';
import ServerStatusPage from './ServerStatusPage';
import TrackerPage from './TrackerPage/TrackerPage';

// 侧边栏折叠状态 decode：缺失/非法值回落到默认（展开）。
// 模块级函数保证引用稳定（useConfigValue 依赖项要求，避免每次渲染重订阅）。
function decodeSidebarCollapsed(raw: string | null): boolean {
  return isYes(parseYesNo(raw, DEFAULT_PANEL_SIDEBAR_COLLAPSED));
}

// 顶部栏高度：左侧标题栏与右侧顶部导航栏共用，保证两者等高、底部分隔线水平对齐。
const TOP_BAR_HEIGHT = 56;

// 保活菜单：切走不卸载，靠「记忆上次完整路径」在切回时恢复子状态（URL 本身不记得切走页的子状态）。
const KEEPALIVE_MENUS: ReadonlySet<MenuKey> = new Set<MenuKey>(['tracker', 'devWorkbench']);

function PanelApp() {
  const { t } = useTranslation();
  // 活动菜单由 URL pathname 派生（path 仅承载顶层页面；子状态走 query，见 routes.ts）。
  const location = useLocation();
  const navigate = useNavigate();
  const activeMenu = pathToMenu(location.pathname);
  const [repoRefreshTrigger, setRepoRefreshTrigger] = useState(0);
  // tracker 保活：首次切到项目事项管理时置 true（仅升不降），配合下方 display:none 隐藏而非卸载，
  // 保留选中工作空间/项目与已加载列表等全部 state（仅会话内，重启重新初始化）。
  const [trackerMounted, setTrackerMounted] = useState(false);
  // devWorkbench 保活：同 tracker，首次切到开发工作台时置 true（仅升不降），display:none 隐藏保留左树选中/步骤进度。
  const [devWorkbenchMounted, setDevWorkbenchMounted] = useState(false);
  // tracker 三级选择态由 tracker store 持有（命令面板/TrackerPage 共享读写），不再上提到此。
  const currentWorkspaceId = useTrackerStore(s => s.selectedWorkspace?.id ?? null);
  // 选中实体名供命令面板 jump 命令声明式渲染名称注释（id 用于逻辑判断，name 用于展示）。
  const currentWorkspaceName = useTrackerStore(s => s.selectedWorkspace?.name ?? null);
  const currentWorkspaceProjectName = useTrackerStore(s => s.selectedWorkspaceProject?.name ?? null);
  const theme = useTheme();
  // 侧边栏折叠状态：订阅 config（跨重启持久化、多窗口同步）。setAppConfig 触发 app-config-changed 事件，hook 自动回写，无需手动 setState。
  const collapsed = useConfigValue(PANEL_SIDEBAR_COLLAPSED_KEY, decodeSidebarCollapsed, false);
  const toggleCollapsed = () => {
    void setAppConfig(PANEL_SIDEBAR_COLLAPSED_KEY, toYesNo(!collapsed));
  };

  // 记忆每个保活菜单的「上次完整路径（含 query）」：切走后再切回时恢复到子状态，而非基础路径。
  // 这是「子状态入 URL」与「保活」共存的关键补偿——URL 不会替切走的页面保留子状态。
  const lastPathRef = useRef<Partial<Record<MenuKey, string>>>({});
  useEffect(() => {
    if (KEEPALIVE_MENUS.has(activeMenu)) {
      lastPathRef.current[activeMenu] = location.pathname + location.search;
    }
  }, [activeMenu, location.pathname, location.search]);

  // 统一菜单跳转：保活菜单回到记忆的上次完整路径（恢复子状态），其余回基础路径。
  // 命令面板 navigate、侧栏点击、后端 panel:navigate 三条入口同源，皆经此。
  const goMenu = useCallback((menu: MenuKey) => {
    navigate(lastPathRef.current[menu] ?? menuToPath(menu));
  }, [navigate]);

  // 保活页首次访问时常驻挂载（仅升不降）：渲染期据 activeMenu 调整（React 推荐模式，避免 effect 内同步 setState）。
  if (activeMenu === 'tracker' && !trackerMounted) {
    setTrackerMounted(true);
  } else if (activeMenu === 'devWorkbench' && !devWorkbenchMounted) {
    setDevWorkbenchMounted(true);
  }

  // 根路径规范化：'/' → 默认页（URL 干净，便于 reload 恢复）。
  useEffect(() => {
    if (location.pathname === '/') {
      navigate(menuToPath(DEFAULT_MENU), { replace: true });
    }
  }, [location.pathname, navigate]);

  const openSettings = useCallback(() => {
    void commands.showSettingsWindow().then((res) => {
      if (res.status === 'error') {
        console.warn('[PanelApp] open settings failed:', res.error);
      }
    });
  }, []);
  // 跳到工作空间：回写选中（清空项目避免跨空间残留）并导航 /tracker?wid=<id>（项目选中态保留 store，不入 URL）。
  const selectWorkspace = useCallback((ws: WorkspaceModel) => {
    useTrackerStore.getState().selectWorkspace(ws); // 联动清空 workspaceProject 在 store 内
    navigate(`/tracker?${TRACKER_WID_PARAM}=${ws.id}`);
  }, [navigate]);
  // 跳到项目：回写选中项目并切到项目事项管理（项目仅在某工作空间已选中时可达，故 selectedWorkspace 必已存在）。
  const selectWorkspaceProject = useCallback((workspaceProject: WorkspaceProjectModel) => {
    useTrackerStore.getState().selectWorkspaceProject(workspaceProject);
    const wid = useTrackerStore.getState().selectedWorkspace?.id;
    navigate(wid ? `/tracker?${TRACKER_WID_PARAM}=${wid}` : '/tracker');
  }, [navigate]);

  // 监听后端 panel:navigate 事件，复用 goMenu 切到指定页面（含保活路径恢复）。
  useEffect(() => {
    const unlisten = listen<MenuKey>(EVENT_PANEL_NAVIGATE, (e) => {
      goMenu(e.payload);
    });
    return () => {
      unlisten.then(fn => fn()).catch(err => console.warn('[PanelApp] unlisten panel:navigate failed:', err));
    };
  }, [goMenu]);

  // 监听 panel:shown 事件：窗口从隐藏恢复时，仅当当前页面是本地仓库管理时触发刷新。
  // 用 ref 读当前路由，避免旧 activeMenu 闭包陈旧（后端先 emit navigate 再 emit shown，但 navigate 是异步路由跳转）。
  const isRepositories = useMatch('/repositories') != null;
  const isRepositoriesRef = useRef(isRepositories);
  useEffect(() => {
    isRepositoriesRef.current = isRepositories;
  }, [isRepositories]);
  useEffect(() => {
    const unlisten = listen(EVENT_PANEL_SHOWN, () => {
      if (isRepositoriesRef.current) {
        setRepoRefreshTrigger(prev => prev + 1);
      }
    });
    return () => {
      unlisten.then(fn => fn()).catch(err => console.warn('[PanelApp] unlisten panel:shown failed:', err));
    };
  }, []);

  const menuItems: { key: MenuKey; label: string; icon: React.ReactNode }[] = [
    { key: 'claudeSessions', label: t('panel:menu.claudeSessions'), icon: <SensorsOutlinedIcon /> },
    { key: 'serverStatus', label: t('panel:menu.serverStatus'), icon: <LanOutlinedIcon /> },
    { key: 'repositories', label: t('panel:menu.repositories'), icon: <FolderOutlinedIcon /> },
    { key: 'tracker', label: t('panel:menu.tracker'), icon: <SpaceDashboardOutlinedIcon /> },
    { key: 'devWorkbench', label: t('panel:menu.devWorkbench'), icon: <DeveloperModeOutlinedIcon /> },
  ];
  // 侧栏隐藏菜单：不渲染入口但页面仍可达（服务状态经顶栏指示器跳转进入）。
  const SIDEBAR_HIDDEN: ReadonlySet<MenuKey> = new Set<MenuKey>(['serverStatus']);
  // 顶部导航栏页面标题：当前激活菜单项 label（含隐藏菜单，故仍从全量 menuItems 查）；单层面包屑，预留未来主/子菜单扩展。
  const activeLabel = menuItems.find(item => item.key === activeMenu)?.label ?? '';

  return (
    <CommandPaletteProvider
      activeMenu={activeMenu}
      navigate={goMenu}
      openSettings={openSettings}
      toggleSidebar={toggleCollapsed}
      currentWorkspaceId={currentWorkspaceId}
      currentWorkspaceName={currentWorkspaceName}
      currentWorkspaceProjectName={currentWorkspaceProjectName}
      selectWorkspace={selectWorkspace}
      selectWorkspaceProject={selectWorkspaceProject}
    >
      <Box sx={{ display: 'flex', height: '100vh', overflow: 'hidden' }}>
        <Box
          sx={{
            width: collapsed ? 56 : 200,
            flexShrink: 0,
            borderRight: 1,
            borderColor: 'divider',
            display: 'flex',
            flexDirection: 'column',
            bgcolor: 'background.paper',
            overflow: 'hidden',
            transition: theme.transitions.create('width', {
              duration: theme.transitions.duration.standard,
              easing: theme.transitions.easing.sharp,
            }),
          }}
        >
          {/* 展开态：pl:3 = 24px = List px:1(8) + ListItemButton paddingLeft(16)，logo 容器宽 36px
            复刻 ListItemIcon minWidth，使 logo / 标题与下方菜单项 icon / 文字分别垂直对齐。
            折叠态：仅居中显示 logo，隐藏标题文字。 */}
          <Box
            sx={{
              height: TOP_BAR_HEIGHT,
              flexShrink: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: collapsed ? 'center' : 'flex-start',
              pl: collapsed ? 0 : 3,
              pr: collapsed ? 0 : 2,
              borderBottom: 1,
              borderColor: 'divider',
            }}
          >
            <Box sx={{ width: 36, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <Box
                component="img"
                src={appIcon}
                alt={t('common:brand')}
                sx={{ width: 20, height: 20, borderRadius: 0.5 }}
              />
            </Box>
            {!collapsed && (
              <Typography variant="body2" sx={{ fontWeight: 600, whiteSpace: 'nowrap' }} color="text.secondary">
                {t('panel:title')}
              </Typography>
            )}
          </Box>
          <List sx={{ px: collapsed ? 0 : 1 }}>
            {menuItems.filter(item => !SIDEBAR_HIDDEN.has(item.key)).map(item => (
              <ListItemButton
                key={item.key}
                selected={activeMenu === item.key}
                onClick={() => goMenu(item.key)}
                {...(collapsed ? { 'aria-label': item.label } : {})}
                sx={{
                  'borderRadius': 2,
                  'mb': 0.5,
                  'justifyContent': collapsed ? 'center' : 'flex-start',
                  'px': collapsed ? 0 : 2,
                  '&.Mui-selected': {
                    bgcolor:
                    theme.palette.mode === 'light'
                      ? alpha(theme.palette.primary.main, 0.15)
                      : alpha(theme.palette.primary.main, 0.35),
                  },
                  '&.Mui-selected:hover': {
                    bgcolor:
                    theme.palette.mode === 'light'
                      ? alpha(theme.palette.primary.main, 0.15)
                      : alpha(theme.palette.primary.main, 0.35),
                  },
                  '& .MuiListItemText-primary': {
                    fontWeight: 600,
                    fontSize: '0.875rem',
                    whiteSpace: 'nowrap',
                  },
                }}
              >
                <Tooltip title={collapsed ? item.label : ''} placement="right" disableInteractive>
                  <ListItemIcon
                    sx={{
                      minWidth: collapsed ? 0 : 36,
                      justifyContent: 'center',
                      color: 'text.primary',
                    }}
                  >
                    {item.icon}
                  </ListItemIcon>
                </Tooltip>
                {!collapsed && <ListItemText primary={item.label} />}
              </ListItemButton>
            ))}
          </List>
          {/* 底部折叠切换按钮：mt:auto 推到侧边栏底部，展开态 ChevronLeft / 折叠态 ChevronRight。 */}
          <Box
            sx={{
              mt: 'auto',
              borderTop: 1,
              borderColor: 'divider',
              display: 'flex',
              justifyContent: 'center',
              py: 0.5,
            }}
          >
            <IconButton
              onClick={toggleCollapsed}
              size="small"
              aria-label={collapsed ? t('panel:sidebar.expand') : t('panel:sidebar.collapse')}
              sx={{ color: 'text.secondary' }}
            >
              {collapsed ? <ChevronRightIcon /> : <ChevronLeftIcon />}
            </IconButton>
          </Box>
        </Box>

        <Box
          sx={{
            flex: 1,
            display: 'flex',
            flexDirection: 'column',
            overflow: 'hidden',
            bgcolor: 'background.default',
          }}
        >
          {/* 顶部导航栏：固定高度，与左侧标题栏等高；底部分隔线与左侧标题/菜单分隔线水平对齐。 */}
          <Box
            sx={{
              height: TOP_BAR_HEIGHT,
              flexShrink: 0,
              display: 'flex',
              alignItems: 'center',
              gap: 1,
              px: 2,
              borderBottom: 1,
              borderColor: 'divider',
              bgcolor: 'background.paper',
            }}
          >
            <Breadcrumbs aria-label="breadcrumb">
              <Typography variant="body2" sx={{ fontWeight: 600 }}>
                {activeLabel}
              </Typography>
            </Breadcrumbs>
            <Box sx={{ flex: 1 }} />
            <CommandPaletteTrigger />
            <ServerStatusIndicator />
            <IconButton
              size="small"
              aria-label={t('settings:title')}
              onClick={openSettings}
              sx={{ color: 'text.secondary' }}
            >
              <SettingsOutlinedIcon />
            </IconButton>
          </Box>
          {/* 页面内容区：各页面自带 header 原样保留。 */}
          <Box sx={{ flex: 1, overflow: 'hidden' }}>
            {activeMenu === 'claudeSessions' && <ClaudeSessionsPage />}
            {activeMenu === 'repositories' && <RepositoriesPage windowShownTrigger={repoRefreshTrigger} />}
            {activeMenu === 'serverStatus' && <ServerStatusPage />}
            {/* tracker 保活：首次访问才挂载，之后常驻；切走用 display:none 隐藏，保留全部 state。 */}
            {trackerMounted && (
              <Box sx={{ height: '100%', display: activeMenu === 'tracker' ? 'block' : 'none' }}>
                <TrackerPage />
              </Box>
            )}
            {devWorkbenchMounted && (
              <Box sx={{ height: '100%', display: activeMenu === 'devWorkbench' ? 'block' : 'none' }}>
                <DevWorkbenchPage />
              </Box>
            )}
          </Box>
        </Box>
      </Box>
    </CommandPaletteProvider>
  );
}

export default PanelApp;
