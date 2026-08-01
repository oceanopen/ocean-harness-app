import type { WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import type { MenuKey } from './command-palette/types';
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
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
} from '@src/shared/app_config';
import { commands } from '@src/shared/bindings';
import { EVENT_PANEL_NAVIGATE, EVENT_PANEL_SHOWN } from '@src/shared/events';
import { useConfigValue } from '@src/shared/useConfigValue';
import { listen } from '@tauri-apps/api/event';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ClaudeSessionsPage from './ClaudeSessionsPage';
import CommandPaletteProvider from './command-palette/CommandPaletteProvider';
import CommandPaletteTrigger from './command-palette/CommandPaletteTrigger';
import RepositoriesPage from './RepositoriesPage';
import ServerStatusPage from './ServerStatusPage';
import TrackerPage from './tracker/TrackerPage';

// 侧边栏折叠状态 decode：缺失/非法值回落到默认（展开）。
// 模块级函数保证引用稳定（useConfigValue 依赖项要求，避免每次渲染重订阅）。
function decodeSidebarCollapsed(raw: string | null): boolean {
  return isYes(parseYesNo(raw, DEFAULT_PANEL_SIDEBAR_COLLAPSED));
}

// panel 窗口（控制台）是各种场景管理页面的通用容器。
// 左侧菜单 + 右侧内容的交互复刻自 settings 窗口（SettingsApp）：
// useState<MenuKey> 单状态 + menuItems 配置数组驱动左侧 List + 右侧条件渲染。
// 当前菜单：Claude 会话监听、本地仓库管理；后续在此数组追加新菜单项即可扩展。
// MenuKey 见 ./command-palette/types（顶级菜单标识与命令面板共用，避免 union 字面量双份维护）。

// 顶部栏高度：左侧标题栏与右侧顶部导航栏共用，保证两者等高、底部分隔线水平对齐。
const TOP_BAR_HEIGHT = 56;

function PanelApp() {
  const { t } = useTranslation();
  const [activeMenu, setActiveMenu] = useState<MenuKey>('claudeSessions');
  const [repoRefreshTrigger, setRepoRefreshTrigger] = useState(0);
  // tracker 保活：首次切到工作台时置 true（仅升不降），配合下方 display:none 隐藏而非卸载，
  // 保留选中工作空间/项目与已加载列表等全部 state（仅会话内，重启重新初始化）。
  const [trackerMounted, setTrackerMounted] = useState(false);
  // tracker 三级选择状态上提到此：命令面板「跳到工作空间/项目」需回写同一份状态，
  // TrackerPage 改为受控消费（selected/selectedProject 经 props 传入）。
  const [selectedWorkspace, setSelectedWorkspace] = useState<WorkspaceModel | null>(null);
  const [selectedProject, setSelectedProject] = useState<WorkspaceProjectModel | null>(null);
  const theme = useTheme();
  // 侧边栏折叠状态：订阅 config（跨重启持久化、多窗口同步）。setAppConfig 触发 app-config-changed 事件，hook 自动回写，无需手动 setState。
  const collapsed = useConfigValue(PANEL_SIDEBAR_COLLAPSED_KEY, decodeSidebarCollapsed, false);
  const toggleCollapsed = () => {
    void setAppConfig(PANEL_SIDEBAR_COLLAPSED_KEY, toYesNo(!collapsed));
  };
  // 命令面板宿主导航/动作回调。navigate 复用顶级菜单切换（含 tracker 首挂载），与侧栏点击同源。
  const navigate = useCallback((menu: MenuKey) => {
    setActiveMenu(menu);
    if (menu === 'tracker') {
      setTrackerMounted(true);
    }
  }, []);
  const openSettings = useCallback(() => {
    void commands.showSettingsWindow().then((res) => {
      if (res.status === 'error') {
        console.warn('[PanelApp] open settings failed:', res.error);
      }
    });
  }, []);
  // 跳到工作空间：回写选中（清空项目避免跨空间残留）并切到工作台。
  const selectWorkspace = useCallback((ws: WorkspaceModel) => {
    setSelectedWorkspace(ws);
    setSelectedProject(null);
    setActiveMenu('tracker');
    setTrackerMounted(true);
  }, []);
  // 跳到项目：回写选中项目并切到工作台（项目仅在某工作空间已选中时可达，故 selectedWorkspace 必已存在）。
  const selectProject = useCallback((project: WorkspaceProjectModel) => {
    setSelectedProject(project);
    setActiveMenu('tracker');
    setTrackerMounted(true);
  }, []);

  // 监听后端 panel:navigate 事件，切换到指定页面（如 pet 点击打开控制台时自动导航到 Claude 会话监听页）。
  useEffect(() => {
    const unlisten = listen<MenuKey>(EVENT_PANEL_NAVIGATE, (e) => {
      setActiveMenu(e.payload);
      // 首次切到工作台时挂载 tracker（保活），在事件源头标记，避免 effect 里 setState。
      if (e.payload === 'tracker') {
        setTrackerMounted(true);
      }
    });
    return () => {
      unlisten.then(fn => fn()).catch(err => console.warn('[PanelApp] unlisten panel:navigate failed:', err));
    };
  }, []);

  // 监听 panel:shown 事件：窗口从隐藏恢复时，仅当当前页面是本地仓库管理时触发刷新。
  // 后端先 emit navigate 再 emit shown，确保此处 activeMenu 已是最新值。
  useEffect(() => {
    const unlisten = listen(EVENT_PANEL_SHOWN, () => {
      if (activeMenu === 'repositories') {
        setRepoRefreshTrigger(prev => prev + 1);
      }
    });
    return () => {
      unlisten.then(fn => fn()).catch(err => console.warn('[PanelApp] unlisten panel:shown failed:', err));
    };
  }, [activeMenu]);

  const menuItems: { key: MenuKey; label: string; icon: React.ReactNode }[] = [
    { key: 'claudeSessions', label: t('panel:menu.claudeSessions'), icon: <SensorsOutlinedIcon /> },
    { key: 'repositories', label: t('panel:menu.repositories'), icon: <FolderOutlinedIcon /> },
    { key: 'serverStatus', label: t('panel:menu.serverStatus'), icon: <LanOutlinedIcon /> },
    { key: 'tracker', label: t('panel:menu.tracker'), icon: <SpaceDashboardOutlinedIcon /> },
  ];
  // 顶部导航栏页面标题：当前激活菜单项 label；单层面包屑，预留未来主/子菜单扩展。
  const activeLabel = menuItems.find(item => item.key === activeMenu)?.label ?? '';

  return (
    <CommandPaletteProvider
      activeMenu={activeMenu}
      navigate={navigate}
      openSettings={openSettings}
      toggleSidebar={toggleCollapsed}
      currentWorkspaceId={selectedWorkspace?.id ?? null}
      selectWorkspace={selectWorkspace}
      selectProject={selectProject}
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
            {menuItems.map(item => (
              <ListItemButton
                key={item.key}
                selected={activeMenu === item.key}
                onClick={() => {
                  setActiveMenu(item.key);
                  if (item.key === 'tracker') {
                    setTrackerMounted(true);
                  }
                }}
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
                <TrackerPage
                  selected={selectedWorkspace}
                  selectedProject={selectedProject}
                  onSelectWorkspace={setSelectedWorkspace}
                  onSelectProject={setSelectedProject}
                />
              </Box>
            )}
          </Box>
        </Box>
      </Box>
    </CommandPaletteProvider>
  );
}

export default PanelApp;
