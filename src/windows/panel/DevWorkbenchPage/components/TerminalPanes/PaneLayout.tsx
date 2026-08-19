import type { PaneLayoutNode } from './types';
import { Box } from '@mui/material';
import { ensureLayout, useTerminalPanesStore } from '@src/state/terminalPanes';
import EmbeddedTerminal from '@src/windows/panel/DevWorkbenchPage/components/EmbeddedTerminal/EmbeddedTerminal';
import { useCallback } from 'react';
import PaneDivider from './PaneDivider';

interface PaneLayoutProps {
  issueId: string;
  node: PaneLayoutNode;
}

// PaneLayout：布局树递归渲染（docs/terminal_02_split_panes.md §3.4）。
//
// leaf → EmbeddedTerminal（key=paneId，pane 增删只影响兄弟节点重挂载范围）；
// split → flex 容器（horizontal=row 左右分 / vertical=column 上下分）+ 两 children
// 递归 + PaneDivider 拖拽调比例。children 配比：first flex-grow=ratio、second
// flex-grow=1-ratio（flex-basis 0），拖拽只改 flexGrow 数值，不重建子树。
//
// leaf 尺寸变化由各 TerminalView 自带 ResizeObserver → fit → pty_resize（现有链路
// 零改动，天然支持 pane 内 resize）。
export default function PaneLayout({ issueId, node }: PaneLayoutProps) {
  const setRatio = useTerminalPanesStore(s => s.setRatio);

  // 渲染期 hydration（幂等）：store 无记录时从 localStorage 读回（F5 布局还原），
  // 读回值落 store 后本组件随 store 更新重渲染。 zustand v5 selector 引用稳定
  // 契约成立（loadLayout 损坏/缺失返回共享 INITIAL_LAYOUT 常量）。
  ensureLayout(issueId);

  const handleRatioChange = useCallback((ratio: number) => {
    if (node.type === 'split') {
      setRatio(issueId, node.id, ratio);
    }
  }, [issueId, node, setRatio]);

  if (node.type === 'leaf') {
    return <EmbeddedTerminal issueId={issueId} paneId={node.paneId} />;
  }

  const { direction, ratio, children } = node;
  return (
    <Box sx={{ display: 'flex', flexDirection: direction === 'horizontal' ? 'row' : 'column', minHeight: 0, minWidth: 0, height: '100%', width: '100%' }}>
      <Box sx={{ flexGrow: ratio, flexBasis: 0, minWidth: 0, minHeight: 0, overflow: 'hidden' }}>
        <PaneLayout issueId={issueId} node={children[0]} />
      </Box>
      <PaneDivider direction={direction} onRatioChange={handleRatioChange} />
      <Box sx={{ flexGrow: 1 - ratio, flexBasis: 0, minWidth: 0, minHeight: 0, overflow: 'hidden' }}>
        <PaneLayout issueId={issueId} node={children[1]} />
      </Box>
    </Box>
  );
}
