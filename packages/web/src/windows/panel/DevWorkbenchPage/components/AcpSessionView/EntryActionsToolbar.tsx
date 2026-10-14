import type { ReactNode } from 'react';
import { Check as CheckIcon, ContentCopy as ContentCopyIcon } from '@mui/icons-material';
import { Box, IconButton, Tooltip } from '@mui/material';
import { useCopyFeedback } from '../fileViewer/useCopyFeedback';

interface EntryActionsToolbarProps {
  /// 是否展示（粘滞 hover 态由列表驱动，不自行监听鼠标）。
  visible: boolean;
  /// 底部对齐侧：发送条目靠右、回复条目靠左。
  side: 'left' | 'right';
  /// 复制来源文本（调用方按条目类型取定）。
  copyText: string;
  /// 追加操作位（渲染于复制钮右侧），后续刷新/编辑等图标经此扩展。
  children?: ReactNode;
}

/**
 * 消息条目操作工具条：流内常驻占位行（提前占位，显隐仅切 opacity/pointerEvents，
 * 布局零位移），显隐由列表层的粘滞 hover 态驱动。复制为首个操作，反馈复用项目统一
 * 剪贴板模式（成功态图标切换，2 秒回退）。
 */
export default function EntryActionsToolbar({ visible, side, copyText, children }: EntryActionsToolbarProps) {
  const { copied, copy } = useCopyFeedback();
  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'center',
        alignSelf: side === 'right' ? 'flex-end' : 'flex-start',
        marginTop: side === 'right' ? '5px' : '0px',
        opacity: visible ? 1 : 0,
        pointerEvents: visible ? 'auto' : 'none',
        transition: 'opacity 0.15s ease',
      }}
    >
      <Tooltip title={copied ? '已复制' : '复制'}>
        <IconButton
          size="small"
          onClick={() => copy(copyText)}
          aria-label="复制消息内容"
          sx={{ color: 'text.secondary', p: '2px' }}
        >
          {copied
            ? <CheckIcon sx={{ fontSize: 15, color: 'success.main' }} />
            : <ContentCopyIcon sx={{ fontSize: 15 }} />}
        </IconButton>
      </Tooltip>
      {children}
    </Box>
  );
}
