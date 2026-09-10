import { Box } from '@mui/material';
import { layoutFor, useTerminalPanesStore } from '@src/state/terminalPanes';
import PaneLayout from './PaneLayout';

interface TerminalPaneRootProps {
  issueId: string;
}

// TerminalPaneRoot：终端区 split 树容器（docs/terminal_02_split_panes.md §3.4）。
//
// 职责：读 store 渲染该 issue 的布局树。分割与关闭 pane 均由各 pane 内 TerminalView
// 工具栏的按钮承担（EmbeddedTerminal props 闭包自识别目标 pane）：分割 =
// handleSplit（splitPane，左右/上下分屏）；关闭 = handleClose（附加 pane
// ptyShutdown+树剪枝 / main 二次确认）。
export default function TerminalPaneRoot({ issueId }: TerminalPaneRootProps) {
  const layout = useTerminalPanesStore(s => layoutFor(s.layouts[issueId]));

  return (
    <Box sx={{ height: '100%', overflow: 'hidden' }}>
      <PaneLayout issueId={issueId} node={layout} />
    </Box>
  );
}
