import type { ReactNode } from 'react';
import { Check as CheckIcon, ContentCopy as ContentCopyIcon } from '@mui/icons-material';
import { IconButton, Tooltip, Typography } from '@mui/material';
import PanelToolbar from '../PanelToolbar';
import { useCopyFeedback } from './useCopyFeedback';
import { languageLabel } from './viewerKind';

interface ViewerToolbarProps {
  /// 语言名分派依据（与 CodeViewer 的 path 语义一致）。
  path: string;
  /// 文件全文（行数派生 + 复制来源）。
  content: string;
  /// 左段扩展槽（渲染于 meta 之后）：md 视图的预览/源码切换等视图级控件。
  children?: ReactNode;
}

/// 文本行数（仅本组件 meta 位使用，不上导出）：末尾换行符视为行终止符而非新行
/// （"a\nb\n" = 2 行，与 wc -l/编辑器口径一致）；空文件 0 行。
function lineCountOf(content: string): number {
  const body = content.endsWith('\n') ? content.slice(0, -1) : content;
  return body === '' ? 0 : body.split('\n').length;
}

/// 文件预览操作栏（36px，PanelToolbar 带）：左段「语言名 · 行数」meta（halo 同款信息位）
/// + 可选视图控件槽；右段复制全文（useCopyFeedback 统一剪贴板反馈，与 md 代码块头部/
/// 终端复制同源）。md 与代码文件预览共用，保证两类文本预览的操作栏观感与交互完全
/// 同源；图片/binary/tooLarge 不用此栏（各有信息态）。
export default function ViewerToolbar({ path, content, children }: ViewerToolbarProps) {
  const { copied, copy } = useCopyFeedback();

  return (
    <PanelToolbar
      left={(
        <>
          <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0, whiteSpace: 'nowrap' }}>
            {languageLabel(path)}
            {' · '}
            {lineCountOf(content)}
            {' 行'}
          </Typography>
          {children}
        </>
      )}
      right={(
        <Tooltip title={copied ? '已复制' : '复制全文'}>
          <IconButton
            size="small"
            onClick={() => copy(content)}
            aria-label="复制全文"
            sx={{ color: 'text.secondary' }}
          >
            {copied ? <CheckIcon sx={{ fontSize: 16, color: 'success.main' }} /> : <ContentCopyIcon sx={{ fontSize: 16 }} />}
          </IconButton>
        </Tooltip>
      )}
    />
  );
}
