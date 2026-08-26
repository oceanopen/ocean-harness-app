// chat 视图容器（terminal_chat T2.2/T3.1 + claude_orca T4.1 交互卡）：接
// useTranscript 状态机分派（非 ready → 空态，ready → 消息列表）；底部
// composer 可发送/停止（回写 PTY）；顶部手动刷新。waiting 态渲染交互卡
// （T4.1：提问卡替换 composer、审批卡悬于 composer 上方，「切回终端回答」
// banner 退役）。数据由 useTranscript 订阅驱动。

import type { AskAnswerSelection, AskPrompt } from './chatAsk';
import { RefreshOutlined as RefreshIcon } from '@mui/icons-material';
import { Box, Button, IconButton, Typography } from '@mui/material';
import { useCallback, useState } from 'react';
import { CHAT_STREAMING_ID } from './chatStreaming';
import NativeChatComposer from './NativeChatComposer';
import NativeChatEmptyState from './NativeChatEmptyState';
import NativeChatInteractiveCard from './NativeChatInteractiveCard';
import NativeChatMessageList from './NativeChatMessageList';
import { useTranscript } from './useTranscript';

interface NativeChatViewProps {
  sessionId: string;
  // 切回 terminal 视图（no-claude 空态 / claude-exited banner 的引导动作）。
  onBackToTerminal: () => void;
  // 发送正文（回写 PTY）。
  onSend: (text: string) => void;
  // 停止（ESC 中断）。
  onStop: () => void;
  // 提问卡提交（T4.1）：父层构造按键组并步进写回 PTY。
  onAskAnswer: (prompt: AskPrompt, selections: AskAnswerSelection[]) => void;
  // 写原始按键串（T4.1 审批选项数字 / 取消 ESC）。
  onChatKeys: (raw: string) => void;
  // 中止在途应答链（T4.1 交互卡换新提问时防旧组串场）。
  onInteractiveCancel: () => void;
}

// 顶部窄横幅（claude-exited）：说明文案 + 引导动作按钮。
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

export default function NativeChatView({
  sessionId,
  onBackToTerminal,
  onSend,
  onStop,
  onAskAnswer,
  onChatKeys,
  onInteractiveCancel,
}: NativeChatViewProps) {
  const { state, messages, claudeStatus, notification, sendEcho, refresh } = useTranscript(sessionId);
  // composer 门槛：Idle 才可发送（Waiting=交互卡在场、Busy=响应中，均禁发）；Busy 才可停止。
  const canSend = claudeStatus === 'Idle';
  const isBusy = claudeStatus === 'Busy';
  // waiting = 交互阻塞（审批/提问），T4.1 起由交互卡在场作答。
  const isWaiting = claudeStatus === 'Waiting';
  // 提问卡在场（InteractiveCard 通告）：替换 composer——卡内自由输入行是唯一
  // 输入面（写给 composer 的字节会落到选择器上）。卡离场/视图卸载自动复位。
  // setState 引用稳定，直传（Dispatch 可赋给 (active: boolean) => void）。
  const [questionActive, setQuestionActive] = useState(false);
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

      {/* 兜底链路降级横幅（T4.1）：hook 链路未生效时（useTranscript 回落
          ptyClaudeSession）Waiting 态无 notification → 无交互卡可渲染，恢复
          「切回终端」指引。hook 模式下 Waiting ⇒ notification 非空不变量成立
          （所有置 Waiting 的分支都同时置 notification），此横幅不出现。 */}
      {isWaiting && notification == null && (
        <ChatBanner text="claude 等待输入" action="切回终端回答" onAction={onBackToTerminal} />
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

      {/* 交互卡（T4.1）：waiting 态渲染于 composer 槽位上缘——提问卡即输入面
          （下方 composer 同时摘除），审批卡悬于 composer 上方。作答后本地消隐，
          claude 下一个事件清 waiting。 */}
      {isWaiting && (
        <NativeChatInteractiveCard
          notification={notification}
          onAnswer={onAskAnswer}
          onRawKeys={onChatKeys}
          onCancelPendingKeys={onInteractiveCancel}
          onQuestionActiveChange={setQuestionActive}
        />
      )}

      {/* 底部 composer：发送/停止（T3.1），门槛见上 canSend/isBusy 派生。
          提问卡在场时整块摘除（卡内输入行接管，见 questionActive 注释）。 */}
      {!questionActive && (
        <NativeChatComposer onSend={handleSend} onStop={onStop} canSend={canSend} isBusy={isBusy} />
      )}
    </Box>
  );
}
