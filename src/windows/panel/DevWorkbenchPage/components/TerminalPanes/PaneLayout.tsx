import type { PaneLayoutNode } from './types';
import { Box } from '@mui/material';
import EmbeddedTerminal from '@src/windows/panel/DevWorkbenchPage/components/EmbeddedTerminal/EmbeddedTerminal';

interface PaneLayoutProps {
  issueId: string;
  node: PaneLayoutNode;
}

// PaneLayout：布局树递归渲染（docs/terminal_02_split_panes.md §3.4）。
//
// leaf → EmbeddedTerminal（key=paneId，pane 增删只影响兄弟节点重挂载范围）；
// split → flex 容器（horizontal=row 左右分 / vertical=column 上下分）+ 两 children
// 递归 + 中间 divider 占位（本期静态 4px 分隔条；拖拽调比例任务 3 接入）。
// children 配比：first flex-grow=ratio、second flex-grow=1-ratio（flex-basis 0），
// 比例变化（divider 拖拽）只改 flexGrow 数值，不重建子树。
//
// leaf 尺寸变化由各 TerminalView 自带 ResizeObserver → fit → pty_resize（现有链路
// 零改动，天然支持 pane 内 resize）。
export default function PaneLayout({ issueId, node }: PaneLayoutProps) {
  if (node.type === 'leaf') {
    return <EmbeddedTerminal issueId={issueId} paneId={node.paneId} />;
  }

  const { direction, ratio, children } = node;
  return (
    <Box sx={{ display: 'flex', flexDirection: direction === 'horizontal' ? 'row' : 'column', minHeight: 0, minWidth: 0, height: '100%', width: '100%' }}>
      <Box sx={{ flexGrow: ratio, flexBasis: 0, minWidth: 0, minHeight: 0, overflow: 'hidden' }}>
        <PaneLayout issueId={issueId} node={children[0]} />
      </Box>
      {/* divider：静态分隔条（任务 3 换 PaneDivider 拖拽组件） */}
      <Box
        sx={{
          flexShrink: 0,
          width: direction === 'horizontal' ? 4 : '100%',
          height: direction === 'horizontal' ? '100%' : 4,
          bgcolor: 'divider',
        }}
      />
      <Box sx={{ flexGrow: 1 - ratio, flexBasis: 0, minWidth: 0, minHeight: 0, overflow: 'hidden' }}>
        <PaneLayout issueId={issueId} node={children[1]} />
      </Box>
    </Box>
  );
}
