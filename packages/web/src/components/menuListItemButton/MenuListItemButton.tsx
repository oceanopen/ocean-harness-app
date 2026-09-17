import type { ReactNode } from 'react';
import { alpha, ListItemButton, ListItemIcon, ListItemText, Tooltip, useTheme } from '@mui/material';

// 左侧菜单项按钮：panel 侧栏菜单与设置页分区菜单的共享组件（原两处逐字重复的
// ListItemButton 选中态样式收敛于此）。样式差异由 scene 单参数内部分支，不枚举样式 props；
// 仅数据（selected/icon/label）与回调（onClick）保留为 props——对齐 IssueCard 的 viewScene 先例。

// 消费场景：
// - panelSidebar：panel 侧栏菜单（支持折叠态 icon-only + Tooltip，折叠态由 collapsed 数据 prop 驱动）
// - settingsSection：设置页分区菜单（恒展开）
export type MenuListItemButtonScene = 'panelSidebar' | 'settingsSection';

export interface MenuListItemButtonProps {
  scene: MenuListItemButtonScene;
  selected: boolean;
  onClick: () => void;
  icon: ReactNode;
  label: string;
  // panelSidebar 场景的折叠态（icon-only + Tooltip + 居中）；settingsSection 恒展开，忽略此值。
  collapsed?: boolean;
}

export default function MenuListItemButton({
  scene,
  selected,
  onClick,
  icon,
  label,
  collapsed = false,
}: MenuListItemButtonProps) {
  const theme = useTheme();
  // 折叠态（仅 panelSidebar）：icon-only 居中 + Tooltip 补全文字。
  const iconOnly = scene === 'panelSidebar' && collapsed;
  // 选中态背景（明暗双模式 alpha），selected 与 selected:hover 同色防悬停闪变。
  const selectedBg
    = theme.palette.mode === 'light'
      ? alpha(theme.palette.primary.main, 0.15)
      : alpha(theme.palette.primary.main, 0.35);

  return (
    <ListItemButton
      selected={selected}
      onClick={onClick}
      {...(iconOnly ? { 'aria-label': label } : {})}
      sx={{
        'borderRadius': 2,
        'mb': 0.5,
        'justifyContent': iconOnly ? 'center' : 'flex-start',
        'px': iconOnly ? 0 : 2,
        '&.Mui-selected': { bgcolor: selectedBg },
        '&.Mui-selected:hover': { bgcolor: selectedBg },
        '& .MuiListItemText-primary': {
          fontWeight: 600,
          fontSize: '0.875rem',
          whiteSpace: 'nowrap',
        },
      }}
    >
      <Tooltip title={iconOnly ? label : ''} placement="right" disableInteractive>
        <ListItemIcon
          sx={{
            minWidth: iconOnly ? 0 : 36,
            justifyContent: 'center',
            color: 'text.primary',
          }}
        >
          {icon}
        </ListItemIcon>
      </Tooltip>
      {!iconOnly && <ListItemText primary={label} />}
    </ListItemButton>
  );
}
