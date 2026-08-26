// 审批卡（T4.1，对标 orca NativeChatApprovalCard，MUI 化）：PermissionRequest
// 在场时渲染在 composer **上方**（composer 保留——审批期间用户不改发消息，
// 但输入面不消失）。盾牌图标 + 工具名 + toolInput 摘要（等宽截断）+ 动态
// 按钮组（chatAsk.approvalButtons：permission_suggestions 序号按钮，缺省
// 回落「允许 1 / 总是允许 2 / 拒绝 ESC」）。

import type { ClaudeNotification } from '@src/shared/bindings';
import { GppMaybeOutlined as ShieldIcon } from '@mui/icons-material';
import { Box, Button, Typography } from '@mui/material';
import { APPROVAL_DENY, approvalButtons } from './chatAsk';

// toolInput 摘要截断长度：完整 JSON 可达数千字符，卡片只作提示（详情看终端）。
const TOOL_INPUT_SUMMARY_LIMIT = 200;

interface NativeChatApprovalCardProps {
  notification: ClaudeNotification;
  // 选中按钮的字面按键串（'1'/'2'/ESC）写回 PTY。
  onChoose: (send: string) => void;
}

export default function NativeChatApprovalCard({ notification, onChoose }: NativeChatApprovalCardProps) {
  const buttons = approvalButtons(notification);
  const toolInput = notification.toolInput;
  const summary = toolInput != null && toolInput.length > TOOL_INPUT_SUMMARY_LIMIT
    ? `${toolInput.slice(0, TOOL_INPUT_SUMMARY_LIMIT)}…`
    : toolInput;

  return (
    <Box sx={{ flexShrink: 0, px: 1, pt: 1 }}>
      <Box
        sx={{
          display: 'flex',
          flexDirection: 'column',
          gap: 1,
          border: 1,
          borderColor: 'divider',
          borderRadius: 1,
          px: 1.5,
          py: 1,
        }}
      >
        <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1 }}>
          <ShieldIcon color="action" sx={{ mt: 0.25, fontSize: 18, flexShrink: 0 }} />
          <Box sx={{ minWidth: 0 }}>
            <Typography variant="body2" sx={{ fontWeight: 600, wordBreak: 'break-word' }}>
              允许 {notification.toolName ?? '未知工具'}？
            </Typography>
            {notification.message != null && notification.message !== '' && (
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', wordBreak: 'break-word' }}>
                {notification.message}
              </Typography>
            )}
          </Box>
        </Box>
        {summary != null && (
          <Typography
            variant="caption"
            component="pre"
            sx={{
              m: 0,
              p: 0,
              fontFamily: 'monospace',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-all',
              maxHeight: '3em',
              overflow: 'hidden',
              color: 'text.secondary',
            }}
          >
            {summary}
          </Typography>
        )}
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1 }}>
          {buttons.map((button, i) => (
            <Button
              key={button.label}
              size="small"
              variant={i === 0 ? 'contained' : 'outlined'}
              color={button.send === APPROVAL_DENY ? 'error' : 'primary'}
              onClick={() => onChoose(button.send)}
            >
              {button.label}
            </Button>
          ))}
        </Box>
      </Box>
    </Box>
  );
}
