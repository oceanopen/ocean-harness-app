import type { ReactNode } from 'react';
import { Box } from '@mui/material';
import { PANEL_TOOLBAR_HEIGHT } from '../workbenchLayout';

interface PanelToolbarProps {
  /// 左区节点（操作按钮组、统计文字等）。
  left?: ReactNode;
  /// 右区节点（关闭、刷新等收尾操作）。
  right?: ReactNode;
}

/**
 * PanelToolbar：开发工作台「面板操作栏」统一样式载体——终端 pane 工具栏与右侧工具
 * 面板头部共用，保证高度（36px）、水平内边距（px 1）、左右布局（left … flex 撑开 …
 * right）、底部分隔线（与标题栏/tab 头的带状区风格连续）完全一致。
 *
 * 操作按钮规格约定（复用方遵守，图标/文字大小保持一致）：
 * - 按钮：`IconButton size="small"` + 图标 `fontSize="small"`（20px），色调 text.secondary
 * - 文字：`Typography variant="caption"`（主题已覆写为整数 12px，见 AppThemeProvider 字号阶梯；
 *   字号规则：最小 12px、一律整数 px 禁小数，变体默认值即安全值）
 */
export default function PanelToolbar({ left, right }: PanelToolbarProps) {
  return (
    <Box
      sx={{
        height: PANEL_TOOLBAR_HEIGHT,
        flexShrink: 0,
        display: 'flex',
        alignItems: 'center',
        px: 1,
        gap: 0.5,
        borderBottom: 1,
        borderColor: 'divider',
      }}
    >
      {left}
      <Box sx={{ flex: 1 }} />
      {right}
    </Box>
  );
}
