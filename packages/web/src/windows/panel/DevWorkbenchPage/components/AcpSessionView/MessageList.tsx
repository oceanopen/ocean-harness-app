import type { SxProps, Theme } from '@mui/material';
import type { AcpConversationEntry, AcpEntryDisplay } from '@src/services';
import type { VirtualizerHandle } from 'virtua';
import { AttachFileOutlined as AttachFileOutlinedIcon, ForumOutlined as ForumOutlinedIcon } from '@mui/icons-material';
import { Box, Chip, Tooltip, Typography } from '@mui/material';
import { memo, useCallback, useEffect, useRef, useState } from 'react';
import { Streamdown } from 'streamdown';
import { Virtualizer } from 'virtua';
import { proseSx, REHYPE_PLUGINS, REMARK_PLUGINS } from '../fileViewer/streamdownShared';
import EntryActionsToolbar from './EntryActionsToolbar';
import { isStickToBottom } from './stickToBottom';
import ToolCallItem from './ToolCallItem';
import 'streamdown/styles.css';

/**
 * 会话记录条目列表：四类条目 + 虚拟化渲染（挂载内存与会话长度解耦）+ 近底部自动滚底
 * （贴底态才归位，用户上翻不打断）。工具调用展开态外置，防止滚出视口后丢失。
 */
const ITEM_SPACING_PX = 10;
const TOP_MARGIN_PX = 12;

/**
 * 条目 wrapper 基础样式（模块级常量避免每次渲染重建）：纵向布局语境 + 条目间距；
 * 工具条显隐由粘滞 hover 态驱动，不在样式层监听鼠标。
 */
const entryWrapperSx: SxProps<Theme> = {
  paddingBottom: `${ITEM_SPACING_PX}px`,
  display: 'flex',
  flexDirection: 'column',
};
// 带工具条条目的间距覆写：条目间距由工具条占位块自身提供（含其上边距），底部仅留
// 兜底空隙，避免工具条行与下一条目贴死。
const toolbarSpacingSx: SxProps<Theme> = { paddingBottom: '4px' };

// 条目对应工具调用的稳定标识：载荷缺失时回退条目自身标识。
function toolCallIdOf(entry: AcpConversationEntry): string {
  return entry.toolCall?.toolCallId ?? entry.entryId;
}

// 复制来源文本：user 取显示原文（bot display.text 优先），agentMessage 取回复原文
// （thinking 为独立条目，天然不含）；其余类型不出复制钮。
function copyTextOf(entry: AcpConversationEntry): string {
  if (entry.kind === 'user') {
    return entry.display?.text ?? entry.text ?? '';
  }
  return entry.text ?? '';
}

// 单条目按类型分发呈现，未知类型防御性忽略；渲染防流式重渲染扩散。
const EntryItem = memo(({ entry, expanded, onToggleToolCall }: {
  entry: AcpConversationEntry;
  expanded: boolean;
  onToggleToolCall: (toolCallId: string) => void;
}) => {
  switch (entry.kind) {
    case 'user':
      return <UserEntry entry={entry} />;
    case 'agentMessage':
      return (
        <Box sx={{ alignSelf: 'stretch', color: 'text.primary' }}>
          <MarkdownMessage text={entry.text ?? ''} />
        </Box>
      );
    case 'agentThought':
      return (
        <Box sx={{ alignSelf: 'stretch', borderLeft: '2px solid', borderColor: 'divider', pl: '10px' }}>
          <Typography variant="caption" color="text.secondary">思考</Typography>
          <MarkdownMessage text={entry.text ?? ''} messageSx={{ color: 'text.secondary' }} />
        </Box>
      );
    case 'toolCall': {
      const toolCall = entry.toolCall ?? { toolCallId: toolCallIdOf(entry) };
      return (
        <Box sx={{ alignSelf: 'stretch' }}>
          <ToolCallItem toolCall={toolCall} expanded={expanded} onToggle={() => onToggleToolCall(toolCall.toolCallId)} />
        </Box>
      );
    }
  }
});

export default function MessageList({ entries }: { entries: AcpConversationEntry[] }) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const virtRef = useRef<VirtualizerHandle>(null);
  const stickToBottomRef = useRef(true);
  // 仅记录展开项，随 issue 切换归零。
  const [expandedToolCalls, setExpandedToolCalls] = useState<Record<string, boolean>>({});
  // 粘滞 hover 态：移入过的条目工具条保持常显，移入新条目才切换。
  const [hoveredEntryId, setHoveredEntryId] = useState<string | null>(null);
  const hoverEntry = useCallback((entryId: string) => setHoveredEntryId(entryId), []);

  const handleScroll = (offset: number) => {
    const el = scrollRef.current;
    if (el) {
      stickToBottomRef.current = isStickToBottom({ scrollHeight: el.scrollHeight, scrollTop: offset, clientHeight: el.clientHeight });
    }
  };

  const toggleToolCall = useCallback((toolCallId: string) => {
    setExpandedToolCalls(prev => ({ ...prev, [toolCallId]: !prev[toolCallId] }));
  }, []);

  // 贴底归底统一入口：新消息到达与条目尺寸变化共用，流式输出持续跟随。
  const followBottom = useCallback(() => {
    if (stickToBottomRef.current && entries.length > 0) {
      virtRef.current?.scrollToIndex(entries.length - 1, { align: 'end' });
    }
  }, [entries]);

  useEffect(followBottom, [followBottom]);

  if (entries.length === 0) {
    return (
      <Box sx={{ flex: 1, minHeight: '0px', display: 'flex', alignItems: 'center', justifyContent: 'center', p: '16px' }}>
        <Box sx={{ textAlign: 'center' }}>
          <Typography variant="body2" color="text.secondary">会话还没有内容</Typography>
          <Typography variant="caption" color="text.disabled" sx={{ display: 'block', mt: '4px' }}>
            在下方输入任务描述开始与 Agent 协作
          </Typography>
        </Box>
      </Box>
    );
  }

  return (
    // 浏览器原生滚动锚定关闭，定位交由虚拟层负责。
    <Box ref={scrollRef} sx={{ flex: 1, minHeight: '0px', overflowY: 'auto', overflowAnchor: 'none' }}>
      {/* 顶部留白须真实存在，滚动定位声明才成立；px 一律字符串写法。 */}
      <Box sx={{ maxWidth: '860px', mx: 'auto', width: '100%', px: '16px', paddingTop: `${TOP_MARGIN_PX}px` }}>
        <Virtualizer
          ref={virtRef}
          data={entries}
          scrollRef={scrollRef}
          startMargin={TOP_MARGIN_PX}
          onScroll={handleScroll}
          onResize={followBottom}
        >
          {(entry: AcpConversationEntry) => {
            const hasToolbar = entry.kind === 'user' || entry.kind === 'agentMessage';
            return (
              // 条目容器承载纵向布局语境、条目间距与工具条定位语境；整行 hover 触发粘滞显隐。
              <Box
                sx={[entryWrapperSx, ...(hasToolbar ? [toolbarSpacingSx] : [])] as SxProps<Theme>}
                onMouseEnter={hasToolbar ? () => hoverEntry(entry.entryId) : undefined}
              >
                <EntryItem
                  entry={entry}
                  expanded={expandedToolCalls[toolCallIdOf(entry)] === true}
                  onToggleToolCall={toggleToolCall}
                />
                {hasToolbar && (
                  <EntryActionsToolbar
                    visible={hoveredEntryId === entry.entryId}
                    side={entry.kind === 'user' ? 'right' : 'left'}
                    copyText={copyTextOf(entry)}
                  />
                )}
              </Box>
            );
          }}
        </Virtualizer>
      </Box>
    </Box>
  );
}

/**
 * user 条目：IM 来源（source=bot 且携带 display）以「IM 徽标 + 引用块 + 正文原文 +
 * 附件 chips」呈现（显示-发送文本分离——条目 text 的围栏全文只归模型侧）；panel 来源或
 * display 缺失（旧 sidecar + 新前端混跑窗口）原样直显 text。
 */
function UserEntry({ entry }: { entry: AcpConversationEntry }) {
  if (entry.source === 'bot' && entry.display) {
    return <ImUserBubble display={entry.display} />;
  }
  return (
    <Box sx={{ alignSelf: 'flex-end', maxWidth: '85%', bgcolor: 'action.selected', borderRadius: '8px', px: '12px', py: '8px' }}>
      <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{entry.text}</Typography>
    </Box>
  );
}

/**
 * IM 来源 user 气泡：右对齐与 panel 消息同位（同一会话双入口混排），徽标与引用块用
 * text.secondary / action.hover 淡化（对齐 agentThought 视觉语言）；正文为 display.text
 * 原文，空正文（纯引用/附件消息）不渲染正文行——占位文案只存在于模型侧围栏。
 */
function ImUserBubble({ display }: { display: AcpEntryDisplay }) {
  return (
    <Box sx={{ alignSelf: 'flex-end', maxWidth: '85%', bgcolor: 'action.selected', borderRadius: '8px', px: '12px', py: '8px', display: 'flex', flexDirection: 'column', gap: '6px' }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
        <ForumOutlinedIcon sx={{ fontSize: '14px', color: 'text.secondary' }} />
        <Typography variant="caption" color="text.secondary">IM</Typography>
      </Box>
      {display.quote && (
        <Box sx={{ bgcolor: 'action.hover', borderRadius: '4px', px: '8px', py: '4px' }}>
          <Typography variant="caption" color="text.secondary">
            {display.quote.author ? `${display.quote.author}：` : '引用消息'}
          </Typography>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
            {display.quote.text}{display.quote.truncated ? '…' : ''}
          </Typography>
        </Box>
      )}
      {display.text && (
        <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{display.text}</Typography>
      )}
      {display.files && display.files.length > 0 && (
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: '4px' }}>
          {display.files.map(f => (
            <Tooltip key={f.path || f.name} title={f.path ?? ''}>
              <Chip size="small" icon={<AttachFileOutlinedIcon />} label={f.name || f.path} sx={{ 'height': '20px', 'fontSize': '12px', '& .MuiChip-icon': { fontSize: '14px' } }} />
            </Tooltip>
          ))}
        </Box>
      )}
    </Box>
  );
}

// agent 消息 markdown 渲染：MarkdownViewer 同款排版（proseSx）与插件链（模块级常量防击穿
// 块级 memo），无 math/mermaid、无自绘代码块；streaming 模式增量解析流式更新（fileViewer
// 静态场景仍 static）。TODO：随会话视图演进按需补 math/mermaid 与代码块复制控件。
function MarkdownMessage({ text, messageSx }: { text: string; messageSx?: SxProps<Theme> }) {
  return (
    <Box sx={[proseSx, { '& p': { my: '6px', lineHeight: 1.7 } }, ...(messageSx ? [messageSx] : [])] as SxProps<Theme>}>
      <Streamdown
        mode="streaming"
        // controls 全关：streamdown 块控件样式依赖 tailwind，本项目无 tailwind（同 MarkdownViewer）。
        controls={false}
        // shikiTheme 兜底：内置代码块缺省 themes 会同步崩溃（同 MarkdownViewer 注释）。
        shikiTheme={['github-light', 'github-dark']}
        remarkPlugins={REMARK_PLUGINS}
        rehypePlugins={REHYPE_PLUGINS}
      >
        {text}
      </Streamdown>
    </Box>
  );
}
