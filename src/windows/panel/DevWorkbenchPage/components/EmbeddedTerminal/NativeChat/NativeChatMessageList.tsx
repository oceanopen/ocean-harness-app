// chat 消息列表（terminal_chat T2.2 + T3.1 吸附滚动）：TranscriptMessage[] → 气泡列表。
// role 分流（user 右 / assistant 左 / tool·system 弱化旁注）；block 分段渲染：
// text→markdown、thinking/tool-call/tool-result→折叠区（默认收起）。
// 底部吸附（T3.1）：用户在底部时新消息/流式 tick 自动跟随到底；向上滚动查看
// 历史即脱附（不被流式滚动劫持），手动滚回底部恢复吸附。

import type { TranscriptBlock, TranscriptMessage } from '@src/shared/bindings';
import type { ReactNode } from 'react';
import { Box, Collapse, Typography } from '@mui/material';
import { useEffect, useRef, useState } from 'react';
import ChatMarkdown from './ChatMarkdown';

// 距底不足此值（px）视为「在底部」（亚像素舍入/滚动惯性余量）。
const BOTTOM_STICK_THRESHOLD = 24;

interface NativeChatMessageListProps {
  messages: TranscriptMessage[];
  // claude busy 中（进行中 turn）：列表末尾渲染「正在生成」占位气泡。
  streaming: boolean;
}

export default function NativeChatMessageList({ messages, streaming }: NativeChatMessageListProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  // 吸附态：ref 而非 state——滚动事件高频，入 state 会整列表抖动重渲染。
  const stickToBottomRef = useRef(true);

  // 新消息 / 流式 tick / 占位气泡出现时：吸附态才跟随到底（瞬时跳转——
  // smooth 在高频 tick 下动画堆积反而滞后）。messages 为 memo 合成数组，
  // 真实落地、echo 上屏、preview 前进均换引用，恰为需要跟随的时机。
  useEffect(() => {
    const el = containerRef.current;
    if (el != null && stickToBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [messages, streaming]);

  return (
    <Box
      ref={containerRef}
      onScroll={(e) => {
        const el = e.currentTarget;
        stickToBottomRef.current
          = el.scrollHeight - el.scrollTop - el.clientHeight < BOTTOM_STICK_THRESHOLD;
      }}
      sx={{
        flex: 1,
        minHeight: 0,
        overflowY: 'auto',
        display: 'flex',
        flexDirection: 'column',
        gap: 1.5,
        px: 1.5,
        py: 1.5,
      }}
    >
      {messages.map(message => (
        <MessageRow key={message.id} message={message} />
      ))}
      {streaming && <StreamingRow />}
    </Box>
  );
}

function MessageRow({ message }: { message: TranscriptMessage }) {
  const isUser = message.role === 'User';
  const isAssistant = message.role === 'Assistant';
  // Tool / System → 弱化旁注（工具结果回流 / 系统提示，非对话主体）。
  const isMuted = !isUser && !isAssistant;

  return (
    <Box sx={{ display: 'flex', justifyContent: isUser ? 'flex-end' : 'flex-start' }}>
      <Box
        sx={{
          maxWidth: '88%',
          px: 1.25,
          py: 0.75,
          borderRadius: 1.5,
          bgcolor: isUser ? 'primary.dark' : isMuted ? 'transparent' : 'action.hover',
          color: isMuted ? 'text.secondary' : 'text.primary',
          ...(isMuted && { fontStyle: 'italic', fontSize: '12px' }),
        }}
      >
        {message.blocks.map((block, index) => (
          // block 无稳定 id，列表静态不重排，index 作 key 安全（无增删/重排场景）。
          // eslint-disable-next-line react/no-array-index-key
          <BlockRenderer key={`${block.type}-${index}`} block={block} />
        ))}
      </Box>
    </Box>
  );
}

// 打字中占位气泡（terminal_chat T3.2）：claude busy 时显示，step 落地后仍保留
// （直到 turn 结束 busy 解除）。无 token 级文本——transcript 只落完整 step。
function StreamingRow() {
  return (
    <Box sx={{ display: 'flex', justifyContent: 'flex-start' }}>
      <Box
        sx={{
          maxWidth: '88%',
          px: 1.25,
          py: 0.75,
          borderRadius: 1.5,
          bgcolor: 'action.hover',
          color: 'text.secondary',
          fontStyle: 'italic',
          fontSize: '12px',
        }}
      >
        正在生成…
      </Box>
    </Box>
  );
}

function BlockRenderer({ block }: { block: TranscriptBlock }) {
  switch (block.type) {
    case 'text':
      return <ChatMarkdown content={block.text} />;
    case 'thinking':
      return (
        <CollapsibleBlock label="思考过程">
          <ChatMarkdown content={block.text} />
        </CollapsibleBlock>
      );
    case 'toolCall':
      return (
        <CollapsibleBlock label={`工具调用 · ${block.name}`}>
          <PreBlock>{block.input ?? '（无参数）'}</PreBlock>
        </CollapsibleBlock>
      );
    case 'toolResult':
      return (
        <CollapsibleBlock label={`工具结果${block.isError ? ' · 错误' : ''}`}>
          <PreBlock>{block.output}</PreBlock>
        </CollapsibleBlock>
      );
    case 'image':
      return (
        <Typography variant="caption" color="text.secondary">
          📎 图片：{block.alt ?? block.path ?? block.url ?? '附件'}
        </Typography>
      );
  }
}

// 折叠区（默认收起）：点击标题展开/收起。P1 用可点击 Typography，键盘可达性后续补。
function CollapsibleBlock({ label, children }: { label: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <Box sx={{ my: 0.4 }}>
      <Box
        onClick={() => setOpen(o => !o)}
        sx={{
          'cursor': 'pointer',
          'userSelect': 'none',
          'display': 'inline-flex',
          'alignItems': 'center',
          'gap': 0.5,
          '&:hover': { opacity: 0.8 },
        }}
      >
        <Typography variant="caption" color="text.secondary">
          {open ? '▾' : '▸'} {label}
        </Typography>
      </Box>
      <Collapse in={open}>
        <Box sx={{ mt: 0.5, pl: 1.5, borderLeft: '2px solid', borderColor: 'divider' }}>
          {children}
        </Box>
      </Collapse>
    </Box>
  );
}

// 等宽预格式化块（工具参数/结果），保留空白与换行。
function PreBlock({ children }: { children: string }) {
  return (
    <Box
      component="pre"
      sx={{
        m: 0,
        fontFamily: 'Menlo, Monaco, Courier New, monospace',
        fontSize: '12px',
        lineHeight: 1.5,
        whiteSpace: 'pre-wrap',
        wordBreak: 'break-all',
        color: 'text.primary',
        maxHeight: 240,
        overflowY: 'auto',
      }}
    >
      {children}
    </Box>
  );
}
