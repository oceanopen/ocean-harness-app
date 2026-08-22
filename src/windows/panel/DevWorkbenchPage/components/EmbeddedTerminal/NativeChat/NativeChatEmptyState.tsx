// chat 只读视图的非就绪状态（terminal_chat T2.2）：loading / no-claude / empty / error
// 四态统一渲染。图标 + 文案 + 可选动作。

import type { ReactNode } from 'react';
import type { TranscriptState } from './useTranscript';
import { ChatBubbleOutlineOutlined as ChatBubbleOutlineIcon } from '@mui/icons-material';
import { Box, Button, CircularProgress, Typography } from '@mui/material';

interface NativeChatEmptyStateProps {
  state: Exclude<TranscriptState, { status: 'ready' }>;
  // no-claude 态引导「切回终端启动 claude」的动作（切回 terminal 视图）。
  onBackToTerminal?: () => void;
}

export default function NativeChatEmptyState({
  state,
  onBackToTerminal,
}: NativeChatEmptyStateProps) {
  let icon: ReactNode;
  let text: string;
  let action: ReactNode = null;

  switch (state.status) {
    case 'loading':
      icon = <CircularProgress size={20} />;
      text = '正在读取对话记录…';
      break;
    case 'no-claude':
      icon = <ChatBubbleOutlineIcon />;
      text = '终端里尚未运行 claude，请先启动 claude 再查看对话';
      action
        = onBackToTerminal != null
          ? (
              <Button size="small" onClick={onBackToTerminal}>
                切回终端启动 claude
              </Button>
            )
          : null;
      break;
    case 'empty':
      icon = <ChatBubbleOutlineIcon />;
      text = '暂无对话记录（在终端发送消息后此处会显示）';
      break;
    case 'error':
      icon = <ChatBubbleOutlineIcon />;
      text = state.message;
      break;
  }

  return (
    <Box
      sx={{
        flex: 1,
        minHeight: 0,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 1,
        p: 2,
      }}
    >
      <Box sx={{ color: 'text.secondary', display: 'flex' }}>{icon}</Box>
      <Typography variant="caption" color="text.secondary" sx={{ textAlign: 'center' }}>
        {text}
      </Typography>
      {action}
    </Box>
  );
}
