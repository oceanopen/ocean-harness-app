import type { ReactNode } from 'react';
import { Box } from '@mui/material';
import { PANEL_TOOLBAR_HEIGHT } from '../workbenchLayout';

interface PanelToolbarProps {
  // 左区节点（操作按钮组、统计文字等）。
  left?: ReactNode;
  // 右区节点（关闭、刷新等收尾操作）。
  right?: ReactNode;
}

/**
 * PanelToolbar：开发工作台「面板操作栏」统一样式载体——终端 pane 工具栏与右侧工具
 * 面板头部共用，保证高度（36px）、水平内边距、左右布局、底部分隔线完全一致。
 * 操作按钮规格约定（复用方遵守）：按钮 `IconButton size="small"` + 图标 `fontSize="small"`（20px）色调 text.secondary；文字 `Typography variant="caption"`（主题已覆写为整数 12px，最小 12px 一律整数 px 禁小数）。
 */
export default function PanelToolbar({ left, right }: PanelToolbarProps) {
  return (
    <Box
      sx={{
        height: `${PANEL_TOOLBAR_HEIGHT}px`,
        flexShrink: 0,
        display: 'flex',
        alignItems: 'center',
        px: '8px',
        gap: '4px',
        borderBottom: '1px solid',
        borderColor: 'divider',
      }}
    >
      {left}
      <Box sx={{ flex: 1 }} />
      {right}
    </Box>
  );
}
