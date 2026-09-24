import type { ReactNode } from 'react';
import { Check as CheckIcon, ContentCopy as ContentCopyIcon, LinkOutlined as LinkOutlinedIcon } from '@mui/icons-material';
import { IconButton, Tooltip, Typography } from '@mui/material';
import PanelToolbar from '../PanelToolbar';
import { useCopyFeedback } from './useCopyFeedback';
import { languageLabel } from './viewerKind';

interface ViewerToolbarProps {
  /// 语言名分派依据（与 CodeViewer 的 path 语义一致）；同时展示完整相对路径（工作区根
  /// 起算，title 悬浮全显）。
  path: string;
  /// 文件全文（行数派生 + 复制来源）。
  content: string;
  /// 右段扩展槽（渲染于复制钮左侧）：md 的预览/源码、diff 的对比/并排等视图级切换控件。
  children?: ReactNode;
}

/// 文本行数（仅本组件 meta 位使用，不上导出）：末尾换行符视为行终止符而非新行
/// （"a\nb\n" = 2 行，与 wc -l/编辑器口径一致）；空文件 0 行。
function lineCountOf(content: string): number {
  const body = content.endsWith('\n') ? content.slice(0, -1) : content;
  return body === '' ? 0 : body.split('\n').length;
}

/// 文件预览操作栏（36px，PanelToolbar 带）：左段「语言名 · 行数 · 完整相对路径」meta
/// （语言/行数不收缩，路径 noWrap 弹性收缩 + title 全显）；右段视图切换控件槽（复制钮
/// 左侧）+ 复制全文（useCopyFeedback 统一剪贴板反馈，与 md 代码块头部/终端复制同源）。
/// md 与代码文件预览共用，保证两类文本预览的操作栏观感与交互完全同源；图片/binary/
/// tooLarge 不用此栏（各有信息态）。
export default function ViewerToolbar({ path, content, children }: ViewerToolbarProps) {
  // 两个复制钮各自独立反馈实例（复制全文 / 复制路径互不干扰，各自 2 秒回退）。
  const contentCopy = useCopyFeedback();
  const pathCopy = useCopyFeedback();

  return (
    <PanelToolbar
      left={(
        <>
          <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0, whiteSpace: 'nowrap' }}>
            {languageLabel(path)}
            {' · '}
            {lineCountOf(content)}
            {' 行 · '}
          </Typography>
          <Typography variant="caption" noWrap title={path} sx={{ minWidth: 0, flexShrink: 1 }}>
            {path}
          </Typography>
        </>
      )}
      right={(
        <>
          {children}
          <Tooltip title={pathCopy.copied ? '已复制路径' : '复制文件路径'}>
            <IconButton
              size="small"
              onClick={() => pathCopy.copy(path)}
              aria-label="复制文件路径"
              sx={{ color: 'text.secondary' }}
            >
              {pathCopy.copied ? <CheckIcon sx={{ fontSize: 16, color: 'success.main' }} /> : <LinkOutlinedIcon sx={{ fontSize: 16 }} />}
            </IconButton>
          </Tooltip>
          <Tooltip title={contentCopy.copied ? '已复制' : '复制全文'}>
            <IconButton
              size="small"
              onClick={() => contentCopy.copy(content)}
              aria-label="复制全文"
              sx={{ color: 'text.secondary' }}
            >
              {contentCopy.copied ? <CheckIcon sx={{ fontSize: 16, color: 'success.main' }} /> : <ContentCopyIcon sx={{ fontSize: 16 }} />}
            </IconButton>
          </Tooltip>
        </>
      )}
    />
  );
}
