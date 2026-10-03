import type { SxProps, Theme } from '@mui/material';
import type { AcpConversationEntry } from '@src/services';
import { Box, Typography } from '@mui/material';
import { useEffect, useRef } from 'react';
import { Streamdown } from 'streamdown';
import { proseSx, REHYPE_PLUGINS, REMARK_PLUGINS } from '../fileViewer/streamdownShared';
import ToolCallItem from './ToolCallItem';
import 'streamdown/styles.css';

// 会话记录条目列表：四类条目（user 高亮块 / agentMessage markdown / agentThought 淡化 /
// toolCall 折叠卡）+ 近底部自动滚底。滚动策略「先测量后使用」：onScroll 实时测距判定
// 用户是否贴底（48px 阈值），entries 更新时仅在贴底态才归位——用户上翻查阅历史不被打断。
export default function MessageList({ entries }: { entries: AcpConversationEntry[] }) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);

  const handleScroll = () => {
    const el = scrollRef.current;
    if (el) {
      stickToBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
    }
  };

  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickToBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [entries]);

  if (entries.length === 0) {
    return (
      <Box sx={{ flex: 1, minHeight: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', p: 2 }}>
        <Box sx={{ textAlign: 'center' }}>
          <Typography variant="body2" color="text.secondary">会话还没有内容</Typography>
          <Typography variant="caption" color="text.disabled" sx={{ display: 'block', mt: 0.5 }}>
            在下方输入任务描述开始与 Agent 协作
          </Typography>
        </Box>
      </Box>
    );
  }

  return (
    <Box ref={scrollRef} onScroll={handleScroll} sx={{ flex: 1, minHeight: 0, overflowY: 'auto' }}>
      <Box sx={{ maxWidth: 860, mx: 'auto', width: '100%', px: 2, py: 1.5, display: 'flex', flexDirection: 'column', gap: 1.25 }}>
        {entries.map(entry => <EntryItem key={entry.entryId} entry={entry} />)}
      </Box>
    </Box>
  );
}

/** 单条目分发：按 kind 选呈现形态（未知 kind 防御性忽略——wire 扩展不炸前端）。 */
function EntryItem({ entry }: { entry: AcpConversationEntry }) {
  switch (entry.kind) {
    case 'user':
      return (
        <Box sx={{ alignSelf: 'flex-end', maxWidth: '85%', bgcolor: 'action.selected', borderRadius: 2, px: 1.5, py: 1 }}>
          <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{entry.text}</Typography>
        </Box>
      );
    case 'agentMessage':
      return (
        <Box sx={{ alignSelf: 'stretch', color: 'text.primary' }}>
          <MarkdownMessage text={entry.text ?? ''} />
        </Box>
      );
    case 'agentThought':
      return (
        <Box sx={{ alignSelf: 'stretch', borderLeft: 2, borderColor: 'divider', pl: 1.25 }}>
          <Typography variant="caption" color="text.secondary">思考</Typography>
          <MarkdownMessage text={entry.text ?? ''} messageSx={{ color: 'text.secondary' }} />
        </Box>
      );
    case 'toolCall':
      return (
        <Box sx={{ alignSelf: 'stretch' }}>
          <ToolCallItem toolCall={entry.toolCall ?? { toolCallId: entry.entryId }} />
        </Box>
      );
  }
}

// agent 消息 markdown 渲染：MarkdownViewer 同款排版（proseSx）与插件链（模块级常量防
// 击穿块级 memo），简化自 fileViewer 全功能版——无 math/mermaid 插件、无自绘代码块
// （罕见语言走内置块，proseSx 兜底 pre 规则保证多行不挤行）、链接/图片按默认渲染。
// TODO：随会话视图演进按需补 math/mermaid 与代码块复制控件。
function MarkdownMessage({ text, messageSx }: { text: string; messageSx?: SxProps<Theme> }) {
  return (
    <Box sx={[proseSx, { '& p': { my: '6px', lineHeight: 1.7 } }, ...(messageSx ? [messageSx] : [])] as SxProps<Theme>}>
      <Streamdown
        mode="static"
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
