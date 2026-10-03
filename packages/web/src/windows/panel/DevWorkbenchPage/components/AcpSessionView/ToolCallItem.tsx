import type { AcpToolCallView } from '@src/services';
import { ExpandLess as ExpandLessIcon, ExpandMore as ExpandMoreIcon } from '@mui/icons-material';
import { Box, Chip, CircularProgress, Collapse, Typography } from '@mui/material';
import { useState } from 'react';
import { TOOL_CALL_STATUS_META } from './toolCallStatusMeta';

// 工具调用条目：标题行（title + name + 状态徽章/in_progress 转圈）+ 可折叠的
// rawInput/rawOutput pretty JSON。ACP wire 原样透传，呈现按 status 选择形态
// （view.go ToolCallView 注释同义）；kind 图标一期不做（TODO：随 agent 目录扩展补）。
export default function ToolCallItem({ toolCall }: { toolCall: AcpToolCallView }) {
  const [expanded, setExpanded] = useState(false);
  const meta = toolCall.status ? TOOL_CALL_STATUS_META[toolCall.status] : undefined;
  const title = toolCall.title || toolCall.name || '工具调用';
  const hasPayload = toolCall.rawInput !== undefined || toolCall.rawOutput !== undefined;

  return (
    <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 1, overflow: 'hidden' }}>
      <Box
        onClick={hasPayload ? () => setExpanded(v => !v) : undefined}
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
        <Collapse in={expanded}>
          <Box sx={{ px: 1.25, py: 1, display: 'flex', flexDirection: 'column', gap: 1 }}>
            {toolCall.rawInput !== undefined && <PayloadBlock label="输入" value={toolCall.rawInput} />}
            {toolCall.rawOutput !== undefined && <PayloadBlock label="输出" value={toolCall.rawOutput} />}
          </Box>
        </Collapse>
      )}
    </Box>
  );
}

// pretty JSON 载荷块：字符串值直显（避免引号噪音），其余 JSON.stringify；解析失败兜底 String。
function PayloadBlock({ label, value }: { label: string; value: unknown }) {
  let text: string;
  if (typeof value === 'string') {
    text = value;
  } else {
    try {
      text = JSON.stringify(value, null, 2) ?? String(value);
    } catch {
      text = String(value);
    }
  }
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
        {text}
      </Box>
    </Box>
  );
}
