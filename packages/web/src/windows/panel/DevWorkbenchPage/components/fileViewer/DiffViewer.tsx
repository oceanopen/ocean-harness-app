import type { IssueWorkspaceFileDiffResponseData } from '@src/services';
import { Box, ToggleButton, ToggleButtonGroup, useTheme } from '@mui/material';
import { useState } from 'react';
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued';
import ViewerToolbar from './ViewerToolbar';

interface DiffViewerProps {
  /// 文件路径（语言名分派 + 复制语义沿用文本预览口径）。
  path: string;
  /// getFileDiff 响应（调用方保证 kind=text）。
  data: IssueWorkspaceFileDiffResponseData;
}

/// 单文件未提交变更 diff 视图：react-diff-viewer-continued 渲染 old（HEAD）/new（工作区）
/// 内容对——新增文件 old 为空（全绿）、删除文件 new 为空（全红），词级高亮 + 未变更区折叠
/// （库默认 showDiffOnly，上下文 3 行）。视图形态切换（对比/并排）挂 ViewerToolbar 左段
/// 扩展槽（MarkdownViewer 预览/源码切换同款范式）；明暗随 MUI 主题（useDarkTheme）。
export default function DiffViewer({ path, data }: DiffViewerProps) {
  const theme = useTheme();
  const [splitView, setSplitView] = useState(false);
  const newContent = data.newContent ?? '';

  return (
    <Box sx={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      <ViewerToolbar path={path} content={newContent}>
        <ToggleButtonGroup
          size="small"
          exclusive
          value={splitView ? 'split' : 'unified'}
          onChange={(_, v) => v != null && setSplitView(v === 'split')}
        >
          <ToggleButton value="unified" sx={{ py: 0.25, px: 1 }}>对比</ToggleButton>
          <ToggleButton value="split" sx={{ py: 0.25, px: 1 }}>并排</ToggleButton>
        </ToggleButtonGroup>
      </ViewerToolbar>
      <Box sx={{ flex: 1, minHeight: 0, overflow: 'auto', fontSize: 12 }}>
        <ReactDiffViewer
          oldValue={data.oldContent ?? ''}
          newValue={newContent}
          splitView={splitView}
          useDarkTheme={theme.palette.mode === 'dark'}
          compareMethod={DiffMethod.WORDS}
          hideLineNumbers={false}
        />
      </Box>
    </Box>
  );
}
