import type { MenuKey } from './routes';
import FolderOutlinedIcon from '@mui/icons-material/FolderOutlined';
import HttpOutlinedIcon from '@mui/icons-material/HttpOutlined';
import InfoOutlinedIcon from '@mui/icons-material/InfoOutlined';
import SettingsOutlinedIcon from '@mui/icons-material/SettingsOutlined';
import TerminalOutlinedIcon from '@mui/icons-material/TerminalOutlined';
import {
  alpha,
  Box,
  Breadcrumbs,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Typography,
  useTheme,
} from '@mui/material';
import appIcon from '@src/assets/app-icon.svg';
import { EVENT_SETTINGS_NAVIGATE } from '@src/shared/events';
import { listen } from '@tauri-apps/api/event';
import { useCallback, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import AboutPage from './AboutPage';
import AppConfigPage from './AppConfigPage';
import ProjectConfigPage from './ProjectConfigPage';
import { DEFAULT_MENU, isMenuKey, MENU_PATHS, menuToPath, pathToMenu } from './routes';
import ServiceConfigPage from './ServiceConfigPage';
import TerminalConfigPage from './TerminalConfigPage';

// 顶部栏高度：左侧标题栏与右侧顶部导航栏共用，保证两者等高、底部分隔线水平对齐。
const TOP_BAR_HEIGHT = 56;

function SettingsApp() {
  const { t } = useTranslation();
  // 活动分区由 URL pathname 派生（路由控制页面展示，对齐 panel 窗口）。
  const location = useLocation();
  const navigate = useNavigate();
  const activeMenu = pathToMenu(location.pathname);
  const theme = useTheme();

  // 分区跳转：settings 无子状态，直跳基础 path。
  const goMenu = useCallback((menu: MenuKey) => {
    navigate(menuToPath(menu));
  }, [navigate]);

  // 深链：show_settings_window(navigate_to=Some) 后 emit_to("settings")，宽容解析非法值回落默认分区。
  useEffect(() => {
    const unlisten = listen<string>(EVENT_SETTINGS_NAVIGATE, (e) => {
      navigate(isMenuKey(e.payload) ? menuToPath(e.payload) : menuToPath(DEFAULT_MENU));
    });
    return () => {
      unlisten.then(fn => fn()).catch(err => console.warn('[SettingsApp] unlisten settings:navigate failed:', err));
    };
  }, [navigate]);

  const menuItems: { key: MenuKey; label: string; icon: React.ReactNode }[] = [
    { key: 'appConfig', label: t('settings:menu.appConfig'), icon: <SettingsOutlinedIcon /> },
    { key: 'terminalConfig', label: t('settings:menu.terminalConfig'), icon: <TerminalOutlinedIcon /> },
    { key: 'projectConfig', label: t('settings:menu.projectConfig'), icon: <FolderOutlinedIcon /> },
    { key: 'serviceConfig', label: t('settings:menu.serviceConfig'), icon: <HttpOutlinedIcon /> },
    { key: 'about', label: t('settings:menu.about'), icon: <InfoOutlinedIcon /> },
  ];
  // 顶部导航栏页面标题：当前激活菜单项 label；单层面包屑，预留未来主/子菜单扩展。
  const activeLabel = menuItems.find(item => item.key === activeMenu)?.label ?? '';

  return (
    <Box sx={{ display: 'flex', height: '100vh', overflow: 'hidden' }}>
      <Box
        sx={{
          width: 200,
          flexShrink: 0,
          borderRight: 1,
          borderColor: 'divider',
          display: 'flex',
          flexDirection: 'column',
          bgcolor: 'background.paper',
          overflow: 'hidden',
        }}
      >
        {/* 左上角标题栏：logo + 标题，与右侧顶部导航栏等高，底部分隔线水平对齐。
            pl:3 = List px:1(8) + ListItemButton paddingLeft(16)，logo 容器宽 36px
            复刻 ListItemIcon minWidth，使 logo / 标题与下方菜单项 icon / 文字分别垂直对齐。
            左上角标题用 settings:title（窗口语义「系统设置」，与 OS 窗口标题栏、panel 顶部入口一致）；
            菜单首项用 settings:menu.appConfig（页面语义「应用设置」），二者由此区分。 */}
        <Box
          sx={{
            height: TOP_BAR_HEIGHT,
            flexShrink: 0,
            display: 'flex',
            alignItems: 'center',
            pl: 3,
            pr: 2,
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
          <Typography variant="body2" sx={{ fontWeight: 600, whiteSpace: 'nowrap' }} color="text.secondary">
            {t('settings:title')}
          </Typography>
        </Box>
        <List sx={{ px: 1 }}>
          {menuItems.map(item => (
            <ListItemButton
              key={item.key}
              selected={activeMenu === item.key}
              onClick={() => goMenu(item.key)}
              sx={{
                'borderRadius': 2,
                'mb': 0.5,
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
              <ListItemIcon sx={{ minWidth: 36, color: 'text.primary' }}>
                {item.icon}
              </ListItemIcon>
              <ListItemText primary={item.label} />
            </ListItemButton>
          ))}
        </List>
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
        {/* 顶部导航栏：固定高度，与左侧标题栏等高；仅显示当前页面标题，右侧不放操作按钮。 */}
        <Box
          sx={{
            height: TOP_BAR_HEIGHT,
            flexShrink: 0,
            display: 'flex',
            alignItems: 'center',
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
        </Box>
        {/* 页面内容区：声明式路由（各页面自带 header 原样保留）；'/' 与未知路径 replace 归一到默认分区。 */}
        <Box sx={{ flex: 1, overflow: 'hidden' }}>
          <Routes>
            <Route path="/" element={<Navigate to={menuToPath(DEFAULT_MENU)} replace />} />
            <Route path={MENU_PATHS.appConfig} element={<AppConfigPage />} />
            <Route path={MENU_PATHS.terminalConfig} element={<TerminalConfigPage />} />
            <Route path={MENU_PATHS.projectConfig} element={<ProjectConfigPage />} />
            <Route path={MENU_PATHS.serviceConfig} element={<ServiceConfigPage />} />
            <Route path={MENU_PATHS.about} element={<AboutPage />} />
            <Route path="*" element={<Navigate to={menuToPath(DEFAULT_MENU)} replace />} />
          </Routes>
        </Box>
      </Box>
    </Box>
  );
}

export default SettingsApp;
