import type { ClaudeSessionInfo, TerminalApp } from '@src/shared/bindings';
import { Box, ListItemButton, Tooltip, Typography } from '@mui/material';
import { CLAUDE_SESSION_STATUS_COLOR, CLAUDE_SESSION_STATUS_I18N_KEY } from '@src/shared/claudeSessionStatus';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

// status → i18n key 映射 SSOT（claudeSessionStatus.ts，复用 claudeSessions:status.* 文案），与 ClaudeSessionCard 共用。

// 暂不支持跳转的宿主终端（与 ClaudeSessionCard 保持一致，禁用点击）。
// OceanHarness：本 app 嵌入终端，禁用跳转但不过滤展示。
const UNSUPPORTED_HOST: TerminalApp[] = ['IntelliJ', 'OceanHarness', 'Unknown'];

interface ClaudeSessionItemProps {
  session: ClaudeSessionInfo;
  onClick: (pid: number) => void;
}

function ClaudeSessionItem({ session, onClick }: ClaudeSessionItemProps) {
  const { t } = useTranslation();
  const unsupported = UNSUPPORTED_HOST.includes(session.hostApp);
  const color = CLAUDE_SESSION_STATUS_COLOR[session.status];
  const text = session.projectName || session.cwd;
  const textRef = useRef<HTMLElement>(null);
  const [clamped, setClamped] = useState(false);

  /** 悬停实测是否被行数上限截断，仅截断时以 tooltip 展示全文 */
  const handleTextHover = () => {
    const el = textRef.current;
    setClamped(!!el && el.scrollHeight > el.clientHeight + 1);
  };

  return (
    <ListItemButton
      disabled={unsupported}
      onClick={() => onClick(session.pid)}
      sx={{
        'px': '12px',
        'py': '8px',
        'borderRadius': '4px',
        // 密集列表：hover 高亮 + 选中态收敛
        '&.Mui-disabled': { opacity: 0.5 },
      }}
    >
      <Tooltip title={clamped ? text : ''}>
        <Typography
          ref={textRef}
          onMouseEnter={handleTextHover}
          sx={{
            flex: 1,
            minWidth: '0px',
            fontWeight: 600,
            fontSize: '14px',
            overflow: 'hidden',
            overflowWrap: 'break-word',
            display: '-webkit-box',
            WebkitBoxOrient: 'vertical',
            WebkitLineClamp: 3,
          }}
        >
          {text}
        </Typography>
      </Tooltip>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: '4px', ml: '8px', flexShrink: 0 }}>
        <Box sx={{ width: '8px', height: '8px', borderRadius: '50%', bgcolor: color }} />
        <Typography
          variant="caption"
          sx={{ color, fontWeight: 700, fontSize: '12px' }}
        >
          {t(CLAUDE_SESSION_STATUS_I18N_KEY[session.status])}
        </Typography>
      </Box>
    </ListItemButton>
  );
}

export default ClaudeSessionItem;
