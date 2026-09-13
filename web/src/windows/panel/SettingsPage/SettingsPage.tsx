import type { SettingsSection } from './routes';
import FolderOutlinedIcon from '@mui/icons-material/FolderOutlined';
import HttpOutlinedIcon from '@mui/icons-material/HttpOutlined';
import InfoOutlinedIcon from '@mui/icons-material/InfoOutlined';
import PersonOutlinedIcon from '@mui/icons-material/PersonOutlined';
import SettingsOutlinedIcon from '@mui/icons-material/SettingsOutlined';
import TerminalOutlinedIcon from '@mui/icons-material/TerminalOutlined';
import { Box, List } from '@mui/material';
import MenuListItemButton from '@src/components/menuListItemButton/MenuListItemButton';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import AboutPage from './AboutPage';
import AppConfigPage from './AppConfigPage';
import ProjectConfigPage from './ProjectConfigPage';
import { DEFAULT_SECTION, pathToSection, SECTION_MENUS, SECTION_PATHS, sectionToPath } from './routes';
import ServiceConfigPage from './ServiceConfigPage';
import TerminalConfigPage from './TerminalConfigPage';
import UserProfilePage from './UserProfilePage';

// 分区菜单 icon：与 SECTION_MENUS 的 key 一一对应，仅在菜单渲染处消费。
const SECTION_ICONS: Record<SettingsSection, React.ReactNode> = {
  appConfig: <SettingsOutlinedIcon />,
  terminalConfig: <TerminalOutlinedIcon />,
  projectConfig: <FolderOutlinedIcon />,
  serviceConfig: <HttpOutlinedIcon />,
  userProfile: <PersonOutlinedIcon />,
  about: <InfoOutlinedIcon />,
};

// panel 内嵌的设置页（/settings 隐藏菜单页）：左侧分区菜单 + 右侧分区内容。
// 关窗与导航由 panel 壳统一接管；分区标题由 panel 顶栏两级面包屑承载。
function SettingsPage() {
  const { t } = useTranslation();
  // 活动分区由 URL pathname 派生（对齐 panel 窗口的 URL 单一事实源约定）。
  const location = useLocation();
  const navigate = useNavigate();
  const activeSection = pathToSection(location.pathname);

  // 分区跳转：直跳分区绝对 path（sectionToPath 组合 panel 顶层 '/settings' 前缀）。
  const goSection = useCallback((section: SettingsSection) => {
    navigate(sectionToPath(section));
  }, [navigate]);

  return (
    <Box sx={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
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
        <List sx={{ px: 1 }}>
          {SECTION_MENUS.map(item => (
            <MenuListItemButton
              key={item.key}
              scene="settingsSection"
              selected={activeSection === item.key}
              onClick={() => goSection(item.key)}
              icon={SECTION_ICONS[item.key]}
              label={t(item.labelI18nKey)}
            />
          ))}
        </List>
      </Box>

      {/* 分区内容区：descendant <Routes>（path 为相对段）；index 与未知路径 replace 归一到默认分区。 */}
      <Box
        sx={{
          flex: 1,
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
          bgcolor: 'background.default',
        }}
      >
        <Box sx={{ flex: 1, overflow: 'hidden' }}>
          <Routes>
            <Route index element={<Navigate to={sectionToPath(DEFAULT_SECTION)} replace />} />
            <Route path={SECTION_PATHS.appConfig} element={<AppConfigPage />} />
            <Route path={SECTION_PATHS.terminalConfig} element={<TerminalConfigPage />} />
            <Route path={SECTION_PATHS.projectConfig} element={<ProjectConfigPage />} />
            <Route path={SECTION_PATHS.serviceConfig} element={<ServiceConfigPage />} />
            <Route path={SECTION_PATHS.userProfile} element={<UserProfilePage />} />
            <Route path={SECTION_PATHS.about} element={<AboutPage />} />
            <Route path="*" element={<Navigate to={sectionToPath(DEFAULT_SECTION)} replace />} />
          </Routes>
        </Box>
      </Box>
    </Box>
  );
}

export default SettingsPage;
