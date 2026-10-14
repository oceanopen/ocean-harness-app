import type { AcpToolCallView } from '@src/services';
import { ExpandLess as ExpandLessIcon, ExpandMore as ExpandMoreIcon } from '@mui/icons-material';
import { Box, Button, Chip, CircularProgress, Collapse, Typography } from '@mui/material';
import { useMemo, useState } from 'react';
import { prettyPayloadText, truncatePayloadText } from './payloadText';
import { TOOL_CALL_STATUS_META } from './toolCallStatusMeta';

/**
 * 工具调用条目：标题行 + 可折叠载荷（折叠即卸载，超长截断可展开全文）。
 * 展开态由使用方持有，防止滚出视口后丢失。
 */
export default function ToolCallItem({ toolCall, expanded, onToggle }: {
  toolCall: AcpToolCallView;
  expanded: boolean;
  onToggle: () => void;
}) {
  const meta = toolCall.status ? TOOL_CALL_STATUS_META[toolCall.status] : undefined;
  const title = toolCall.title || toolCall.name || '工具调用';
  const hasPayload = toolCall.rawInput !== undefined || toolCall.rawOutput !== undefined;

  return (
    <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 1, overflow: 'hidden' }}>
      <Box
        onClick={hasPayload ? onToggle : undefined}
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          px: 1.25,
          py: 0.75,
          bgcolor: 'action.hover',
          ...(hasPayload && { cursor: 'pointer' }),
        }}
      >
        {hasPayload
          ? (expanded ? <ExpandLessIcon fontSize="small" color="action" /> : <ExpandMoreIcon fontSize="small" color="action" />)
          : <Box sx={{ width: 20 }} />}
        <Typography variant="body2" sx={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>
          {title}
          {toolCall.name && toolCall.title && toolCall.name !== toolCall.title && (
            <Typography component="span" variant="caption" color="text.secondary" sx={{ ml: 1 }}>
              {toolCall.name}
            </Typography>
          )}
        </Typography>
        {toolCall.status === 'in_progress' && <CircularProgress size={14} />}
        {meta && <Chip size="small" label={meta.label} color={meta.color} sx={{ height: 20, fontSize: 12 }} />}
      </Box>
      {hasPayload && (
        <Collapse in={expanded} unmountOnExit>
          <Box sx={{ px: 1.25, py: 1, display: 'flex', flexDirection: 'column', gap: 1 }}>
            {toolCall.rawInput !== undefined && <PayloadBlock label="输入" value={toolCall.rawInput} />}
            {toolCall.rawOutput !== undefined && <PayloadBlock label="输出" value={toolCall.rawOutput} />}
          </Box>
        </Collapse>
      )}
    </Box>
  );
}

// 载荷块：pretty JSON 文本化 + 超限截断与全文展开，展开态随收起重置。
function PayloadBlock({ label, value }: { label: string; value: unknown }) {
  const [full, setFull] = useState(false);
  const pretty = useMemo(() => prettyPayloadText(value), [value]);
  const { text, truncated } = truncatePayloadText(pretty);
  return (
    <Box>
      <Typography variant="caption" color="text.secondary">{label}</Typography>
      <Box
        component="pre"
        sx={{
          m: 0,
          mt: 0.5,
          p: 1,
          overflowX: 'auto',
          border: 1,
          borderColor: 'divider',
          borderRadius: 0.5,
          bgcolor: 'background.default',
          fontFamily: 'inherit',
          fontSize: 12,
          lineHeight: 1.6,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
        }}
      >
        {full ? pretty : text}
        {truncated && !full && '…'}
      </Box>
      {truncated && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 0.5 }}>
          {!full && (
            <Typography variant="caption" color="text.disabled">已截断，共 {pretty.length} 字符</Typography>
          )}
          <Button size="small" sx={{ minWidth: 0, px: 0.75 }} onClick={() => setFull(v => !v)}>
            {full ? '收起' : '展开全文'}
          </Button>
        </Box>
      )}
    </Box>
  );
}
