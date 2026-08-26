// chat 视图容器（terminal_chat T2.2/T3.1/T3.3）：接 useTranscript 状态机分派（非 ready
// → 空态，ready → 消息列表）；底部 composer 可发送/停止（回写 PTY）；顶部手动刷新。
// waiting 态显示交互 prompt 引导（T3.3）。数据由 useTranscript 订阅驱动。

import { RefreshOutlined as RefreshIcon } from '@mui/icons-material';
import { Box, Button, IconButton, Typography } from '@mui/material';
import { useCallback } from 'react';
import { CHAT_STREAMING_ID } from './chatStreaming';
import NativeChatComposer from './NativeChatComposer';
import NativeChatEmptyState from './NativeChatEmptyState';
import NativeChatMessageList from './NativeChatMessageList';
import { useTranscript } from './useTranscript';

interface NativeChatViewProps {
  sessionId: string;
  // 切回 terminal 视图（no-claude 空态的引导动作）。
  onBackToTerminal: () => void;
  // 发送正文（回写 PTY）。
  onSend: (text: string) => void;
  // 停止（ESC 中断）。
  onStop: () => void;
}

// 顶部窄横幅（waiting / claude-exited 共用形态）：说明文案 + 引导动作按钮。
function ChatBanner({ text, action, onAction }: { text: string; action: string; onAction: () => void }) {
  return (
    <Box
      sx={{
        flexShrink: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 1,
        px: 1.5,
        py: 0.5,
        bgcolor: 'action.hover',
      }}
    >
      <Typography variant="caption" color="text.secondary">{text}</Typography>
      <Button size="small" onClick={onAction}>{action}</Button>
    </Box>
  );
}

export default function NativeChatView({ sessionId, onBackToTerminal, onSend, onStop }: NativeChatViewProps) {
  const { state, messages, claudeStatus, waitingFor, sendEcho, refresh } = useTranscript(sessionId);
  // composer 门槛：Idle 才可发送（Waiting=交互 prompt 阻塞、Busy=响应中，均禁发）；Busy 才可停止。
  const canSend = claudeStatus === 'Idle';
  const isBusy = claudeStatus === 'Busy';
  // waiting = 交互 prompt（权限确认/提问），引导切回终端回答。
  const isWaiting = claudeStatus === 'Waiting';
  // 发送编排（T3.1）：先乐观 echo（立即上屏），真实回写经 onSend 走发送队列
  // （串行防粘行，EmbeddedTerminal chatSendQueue）。
  const handleSend = useCallback((text: string) => {
    sendEcho(text);
    onSend(text);
  }, [sendEcho, onSend]);

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

      {/* waiting banner（terminal_chat T3.3）：claude 交互 prompt 阻塞（权限/提问），
          引导切回终端回答。waitingFor 为 session json 附带的上下文（如 "approve Bash"）。 */}
      {isWaiting && (
        <ChatBanner
          text={`claude 等待输入${waitingFor != null && waitingFor !== '' ? `（${waitingFor}）` : ''}`}
          action="切回终端回答"
          onAction={onBackToTerminal}
        />
      )}

      {/* 主体：状态分派。ready / claude-exited 都有消息列表；claude-exited 额外
          顶部 banner 提示「claude 已退出」+ 切回终端引导（历史仍可看）。 */}
      {state.status === 'ready' || state.status === 'claude-exited'
        ? (
            <>
              {state.status === 'claude-exited' && (
                <ChatBanner text="claude 已退出" action="切回终端启动 claude" onAction={onBackToTerminal} />
              )}
              <NativeChatMessageList
                messages={messages}
                // 「正在生成」占位在有真实流式气泡时让位（合成气泡已表达进行中）。
                streaming={isBusy && !messages.some(m => m.id === CHAT_STREAMING_ID)}
              />
            </>
          )
        : (
            <NativeChatEmptyState state={state} onBackToTerminal={onBackToTerminal} />
          )}

      {/* 底部 composer：发送/停止（T3.1），门槛见上 canSend/isBusy 派生 */}
      <NativeChatComposer onSend={handleSend} onStop={onStop} canSend={canSend} isBusy={isBusy} />
    </Box>
  );
}
