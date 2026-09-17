import type { ClaudeSessionInfo } from '@src/shared/bindings';
import { Box } from '@mui/material';
import { sortClaudeSessions } from '@src/shared/claudeSessionStatus';
import ClaudeSessionCard from './ClaudeSessionCard';
import EmptyState from './EmptyState';

interface ClaudeSessionListProps {
  // 全量会话快照（Dead 理论上不出现，后端 discover 已过滤）。列表内按 CLAUDE_SESSION_STATUS_PRIORITY
  // 排序（SSOT: claudeSessionStatus.ts）：Waiting > GitPending > Busy > Idle > Dead。
  sessions: ClaudeSessionInfo[];
  onOpenTerminal: (pid: number) => void;
}

function ClaudeSessionList({ sessions, onOpenTerminal }: ClaudeSessionListProps) {
  if (sessions.length === 0) {
    return <EmptyState />;
  }
  const ordered = sortClaudeSessions(sessions);
  return (
    <Box
      sx={{
        p: 2,
        display: 'grid',
        gap: 2,
        // 响应式 1-2 列：与 RepositoriesPage 一致（窄屏 1 列，md 起两列）。
        gridTemplateColumns: {
          xs: '1fr',
          sm: 'repeat(1, 1fr)',
          md: 'repeat(2, 1fr)',
          lg: 'repeat(2, 1fr)',
        },
        alignItems: 'start',
      }}
    >
      {ordered.map(s => (
        <ClaudeSessionCard key={s.pid} session={s} onOpenTerminal={onOpenTerminal} />
      ))}
    </Box>
  );
}

export default ClaudeSessionList;
