import {
  HorizontalSplit as HorizontalSplitIcon,
  VerticalSplit as VerticalSplitIcon,
} from '@mui/icons-material';
import { IconButton } from '@mui/material';
import { layoutFor, leafPaneIds, useTerminalPanesStore } from '@src/state/terminalPanes';
import { useCallback } from 'react';

interface TerminalSplitButtonsProps {
  issueId: string;
}

// TerminalSplitButtons：终端分割按钮组（左右分 / 上下分），嵌入 DevWorkbenchPage
// 标题栏右侧快捷区（terminal_02 §3.4 工具条的迁移形态——原终端区工具条与顶部
// 工具栏功能重叠，按用户反馈上移）。分割作用于活跃 pane——focus 跟随
// （EmbeddedTerminal onActive → store.setActivePane）；无焦点记录（首开）或记录
// 已不在树上（pane 被关闭后残留）时回落树上最后一个 leaf。
export default function TerminalSplitButtons({ issueId }: TerminalSplitButtonsProps) {
  const layout = useTerminalPanesStore(s => layoutFor(s.layouts[issueId]));
  const recorded = useTerminalPanesStore(s => s.activePanes[issueId]);
  const splitPane = useTerminalPanesStore(s => s.splitPane);

  const leaves = leafPaneIds(layout);
  // 活跃 pane：焦点记录存在且仍在树上则用之，否则回落最后一个 leaf。
  const activePaneId = recorded != null && leaves.includes(recorded)
    ? recorded
    : leaves[leaves.length - 1] ?? 'main';

  const splitHorizontal = useCallback(() => {
    splitPane(issueId, activePaneId, 'horizontal');
  }, [issueId, activePaneId, splitPane]);
  const splitVertical = useCallback(() => {
    splitPane(issueId, activePaneId, 'vertical');
  }, [issueId, activePaneId, splitPane]);

  return (
    <>
      <IconButton size="small" onClick={splitHorizontal} aria-label="左右分割" sx={{ color: 'text.secondary' }}>
        <VerticalSplitIcon fontSize="small" />
      </IconButton>
      <IconButton size="small" onClick={splitVertical} aria-label="上下分割" sx={{ color: 'text.secondary' }}>
        <HorizontalSplitIcon fontSize="small" />
      </IconButton>
    </>
  );
}
