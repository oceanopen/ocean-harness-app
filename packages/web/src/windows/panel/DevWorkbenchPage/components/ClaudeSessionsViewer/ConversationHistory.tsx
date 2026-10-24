import type { ClaudeEntryKind, ClaudeSessionEntry } from '@src/shared/bindings';
import { Box, CircularProgress, Typography } from '@mui/material';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { useEffect, useState } from 'react';

// 条目 kind → 色点与前缀（简版一行一条，超长省略号兜底）。
const KIND_META: Record<ClaudeEntryKind, { color: string; prefix: string }> = {
  user: { color: 'primary.main', prefix: '' },
  agentMessage: { color: 'success.main', prefix: '' },
  agentThought: { color: 'warning.main', prefix: '[思考] ' },
  toolCall: { color: 'info.main', prefix: '[工具] ' },
  toolResult: { color: 'text.disabled', prefix: '[结果] ' },
};

// 单条展示文本：工具条目优先 toolName+toolSummary，其余用正文（正文空回落摘要）。
function entryLine(entry: ClaudeSessionEntry): string {
  const meta = KIND_META[entry.kind];
  if (entry.kind === 'toolCall') {
    return `${meta.prefix}${entry.toolName}${entry.toolSummary !== '' ? ` ${entry.toolSummary}` : ''}`;
  }
  return `${meta.prefix}${entry.text !== '' ? entry.text : entry.toolSummary}`;
}

interface ConversationHistoryProps {
  claudeSessionId: string;
  // 任务工作区目录（jsonl 定位锚）；null = 容器未解析，降级空态。
  cwd: string | null;
}

/**
 * 会话卡片展开区：本地历史消息首页（jsonl 直读，offset 缺省取首页，无翻页）。
 * 加载中转圈；失败/空/容器未解析一律降级「暂无本地历史」。
 */
export default function ConversationHistory({ claudeSessionId, cwd }: ConversationHistoryProps) {
  const [entries, setEntries] = useState<ClaudeSessionEntry[] | null>(null);

  useEffect(() => {
    if (cwd == null) {
      return;
    }
    let alive = true;
    unwrap(commands.getClaudeSessionMessages(cwd, claudeSessionId, null))
      .then((page) => {
        if (alive) {
          setEntries(page.entries);
        }
      })
      .catch(() => {
        if (alive) {
          setEntries([]);
        }
      });
    return () => {
      alive = false;
    };
  }, [cwd, claudeSessionId]);

  const pending = cwd != null && entries === null;
  if (pending || entries == null || entries.length === 0) {
    return (
      <Box sx={{ borderTop: '1px solid', borderColor: 'divider', display: 'flex', justifyContent: 'center', py: '12px' }}>
        {pending
          ? <CircularProgress size={16} sx={{ color: 'text.secondary' }} />
          : <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>暂无本地历史</Typography>}
      </Box>
    );
  }
  return (
    <Box sx={{ borderTop: '1px solid', borderColor: 'divider', maxHeight: '220px', overflow: 'auto', py: '4px' }}>
      {entries.map(entry => (
        <Box key={entry.entryId} sx={{ display: 'flex', alignItems: 'flex-start', gap: '8px', px: '12px', py: '4px' }}>
          <Box sx={{ width: '8px', height: '8px', borderRadius: '50%', bgcolor: KIND_META[entry.kind].color, flexShrink: 0, mt: '5px' }} />
          <Typography noWrap title={entryLine(entry)} sx={{ fontSize: '12px', color: 'text.secondary', flex: 1, minWidth: '0px' }}>
            {entryLine(entry)}
          </Typography>
        </Box>
      ))}
    </Box>
  );
}
