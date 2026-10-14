import type { ClaudeSessionInfo, TerminalApp } from '@src/shared/bindings';
import { Box, ListItemButton, Typography } from '@mui/material';
import { CLAUDE_SESSION_STATUS_COLOR, CLAUDE_SESSION_STATUS_I18N_KEY } from '@src/shared/claudeSessionStatus';
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
      <Typography
        sx={{
          flex: 1,
          fontWeight: 600,
          fontSize: '14px',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
      >
        {session.projectName || session.cwd}
      </Typography>
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
