import {
  HorizontalSplit as HorizontalSplitIcon,
  VerticalSplit as VerticalSplitIcon,
} from '@mui/icons-material';
import { Box, IconButton } from '@mui/material';
import { layoutFor, leafPaneIds, useTerminalPanesStore } from '@src/state/terminalPanes';
import { useCallback } from 'react';
import PaneLayout from './PaneLayout';

interface TerminalPaneRootProps {
  issueId: string;
}

// TerminalPaneRoot：终端区 split 树容器（docs/terminal_02_split_panes.md §3.4）。
//
// 职责：读 store 渲染该 issue 的布局树 + 顶部工具条（分割按钮；按钮组在模块 3
// 扩展）。分割作用于活跃 pane——本期 focus 跟随未接（任务 4），兜底取树上最后
// 一个 leaf（新 pane 恒为 last，连续分割自然往末端追加，符合直觉）。
//
// 关闭 pane 由各 pane 内 TerminalView 工具栏的「关闭终端」承担（EmbeddedTerminal
// session.close → ptyShutdown + store.closePane 剪枝——接线见 EmbeddedTerminal）。
export default function TerminalPaneRoot({ issueId }: TerminalPaneRootProps) {
  const layout = useTerminalPanesStore(s => layoutFor(s.layouts[issueId]));
  const splitPane = useTerminalPanesStore(s => s.splitPane);

  const leaves = leafPaneIds(layout);
  // 活跃 pane 兜底：树上最后一个 leaf（任务 4 换 activePanes focus 跟随）。
  const activePaneId = leaves[leaves.length - 1] ?? 'main';

  const splitHorizontal = useCallback(() => {
    splitPane(issueId, activePaneId, 'horizontal');
  }, [issueId, activePaneId, splitPane]);
  const splitVertical = useCallback(() => {
    splitPane(issueId, activePaneId, 'vertical');
  }, [issueId, activePaneId, splitPane]);

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* 工具条：左右分 / 上下分（不挂 Tooltip，aria-label 提供语义） */}
      <Box sx={{ height: 28, flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 0.5, px: 0.5, borderBottom: 1, borderColor: 'divider' }}>
        <IconButton size="small" onClick={splitHorizontal} aria-label="左右分割" sx={{ color: 'text.secondary' }}>
          <VerticalSplitIcon fontSize="small" />
        </IconButton>
        <IconButton size="small" onClick={splitVertical} aria-label="上下分割" sx={{ color: 'text.secondary' }}>
          <HorizontalSplitIcon fontSize="small" />
        </IconButton>
      </Box>
      <Box sx={{ flex: 1, minHeight: 0 }}>
        <PaneLayout issueId={issueId} node={layout} />
      </Box>
    </Box>
  );
}
