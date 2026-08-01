import type { ClaudeSessionStatus } from '@src/shared/bindings';
import type { ReactElement } from 'react';
import { Branch } from '@icon-park/react';
import { Autorenew, Bedtime, Notifications, Schedule } from '@mui/icons-material';
import { Box } from '@mui/material';
import { CLAUDE_SESSION_STATUS_COLOR } from '@src/shared/claudeSessionStatus';
import '@icon-park/react/styles/index.css';

interface PetSpriteProps {
  status: ClaudeSessionStatus;
  count: number;
}

// 各状态对应的图标渲染函数。MUI 图标（Autorenew/Notifications/Schedule/Bedtime）走 sx；
// GitPending 用 IconPark 的 Branch（git 分支图标，outline 线性风格），它走 size/fill/theme，
// 故用渲染函数记录统一处理不同图标 API，避免 JSX 内分支。
// 颜色统一跟随 CLAUDE_SESSION_STATUS_COLOR：GitPending 也是 info 蓝（与边框/徽章一致）。
// IconPark 图标默认主题 outline，按需 deep import（@icon-park/react/es/icons/Branch）最小化打包。
const renderIcon: Record<ClaudeSessionStatus, (color: string) => ReactElement> = {
  Busy: color => <Autorenew sx={{ color, fontSize: 56 }} />,
  Waiting: color => <Notifications sx={{ color, fontSize: 56 }} />,
  GitPending: color => <Branch theme="outline" size={42} fill={color} />,
  Idle: color => <Schedule sx={{ color, fontSize: 56 }} />,
  Dead: color => <Bedtime sx={{ color, fontSize: 56 }} />,
};

function PetSprite({ status, count }: PetSpriteProps) {
  const color = CLAUDE_SESSION_STATUS_COLOR[status];

  return (
    <Box
      sx={{
        position: 'relative',
        width: 96,
        height: 96,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        borderRadius: '50%',
        // 半透明背景圆 + 颜色边框，让 icon 在任意桌面背景下都可见。
        bgcolor: 'rgba(255,255,255,0.85)',
        border: `3px solid ${color}`,
        boxShadow: '0 4px 16px rgba(0,0,0,0.2)',
      }}
    >
      {renderIcon[status](color)}
      {import.meta.env.DEV && (
        <Box
          sx={{
            // dev 构建标记：圆内右下角空隙处的红色小圆 + 白边，样式对齐托盘 dev 角标
            // (src/assets/app-icon-dev.svg)。仅 dev 模式显示，避免本地调试版与正式版混淆；
            // 落在圆内、icon 右下方空隙，不挡 icon，与会话数徽章错位不重叠。
            position: 'absolute',
            bottom: 14,
            right: 14,
            width: 12,
            height: 12,
            borderRadius: '50%',
            bgcolor: '#EF4444',
            border: '2px solid #fff',
            boxShadow: '0 1px 3px rgba(0,0,0,0.35)',
          }}
        />
      )}
      {count > 0 && (
        <Box
          sx={{
            position: 'absolute',
            bottom: -4,
            right: -4,
            minWidth: 22,
            height: 22,
            px: 0.5,
            borderRadius: 11,
            bgcolor: color,
            color: '#fff',
            fontSize: 12,
            fontWeight: 700,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            border: '2px solid #fff',
          }}
        >
          {count}
        </Box>
      )}
    </Box>
  );
}

export default PetSprite;
