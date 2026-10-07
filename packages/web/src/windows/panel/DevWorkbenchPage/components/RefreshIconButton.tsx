import type { MouseEventHandler } from 'react';
import AutorenewIcon from '@mui/icons-material/Autorenew';
import { IconButton } from '@mui/material';

interface RefreshIconButtonProps {
  /// 点击刷新（invalidate / refetch 策略由调用方决定）。
  onClick: MouseEventHandler<HTMLButtonElement>;
  /// 取数中（true 禁用防重复 + 图标旋转，取数状态可见）。
  fetching: boolean;
  /// 无障碍标签（各面板语义不同，如「刷新子任务状态」「刷新文件列表」）。
  ariaLabel: string;
}

/**
 * RefreshIconButton：工具面板头部「刷新」按钮统一载体（PanelToolbar 按钮规格约定的具体化：
 * IconButton size="small" + 图标 fontSize="small"、色调 text.secondary；fetching 期禁用 +
 * spin 旋转）。原 IssueSubTaskPanel / WorkspaceFilePanel / ImBotPanel 三份逐字拷贝收敛于此。
 */
export default function RefreshIconButton({ onClick, fetching, ariaLabel }: RefreshIconButtonProps) {
  return (
    <IconButton
      size="small"
      onClick={onClick}
      disabled={fetching}
      aria-label={ariaLabel}
      sx={{ color: 'text.secondary' }}
    >
      <AutorenewIcon
        fontSize="small"
        sx={{
          'animation': fetching ? 'spin 0.8s linear infinite' : undefined,
          '@keyframes spin': {
            from: { transform: 'rotate(0deg)' },
            to: { transform: 'rotate(360deg)' },
          },
        }}
      />
    </IconButton>
  );
}
