// chat 只读视图容器（terminal_chat T2.2）：接 useTranscript 状态机分派（非 ready → 空态，
// ready → 消息列表）；底部 composer 置灰 + 「切回终端发送」提示；顶部手动刷新。
// 挂载即读 + 监听 claude-sessions:changed 自动重读（useTranscript 内部），P1 只读、
// 发消息留 P2。

import { RefreshOutlined as RefreshIcon } from '@mui/icons-material';
import { Box, IconButton, Typography } from '@mui/material';
import NativeChatEmptyState from './NativeChatEmptyState';
import NativeChatMessageList from './NativeChatMessageList';
import { useTranscript } from './useTranscript';

interface NativeChatViewProps {
  sessionId: string;
  // 切回 terminal 视图（no-claude 空态的引导动作）。
  onBackToTerminal: () => void;
}

export default function NativeChatView({ sessionId, onBackToTerminal }: NativeChatViewProps) {
  const { state, refresh } = useTranscript(sessionId);

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 顶部：手动刷新（数据刷新由 useTranscript 事件自动驱动，此处兜底/手动即时重读） */}
      <Box
        sx={{
          height: 28,
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'flex-end',
          px: 0.5,
        }}
      >
        <IconButton size="small" onClick={refresh} aria-label="刷新对话" sx={{ color: 'text.secondary' }}>
          <RefreshIcon fontSize="small" />
        </IconButton>
      </Box>

      {/* 主体：状态分派 */}
      {state.status === 'ready'
        ? (
            <NativeChatMessageList messages={state.messages} />
          )
        : (
            <NativeChatEmptyState state={state} onBackToTerminal={onBackToTerminal} />
          )}

      {/* 底部 composer：P1 置灰只读，提示切回终端发送（P2 接入真正输入框） */}
      <Box
        sx={{
          flexShrink: 0,
          px: 1.5,
          py: 1,
          borderTop: 1,
          borderColor: 'divider',
          opacity: 0.55,
          pointerEvents: 'none',
        }}
      >
        <Typography variant="caption" color="text.secondary">
          切回终端发送消息
        </Typography>
      </Box>
    </Box>
  );
}
