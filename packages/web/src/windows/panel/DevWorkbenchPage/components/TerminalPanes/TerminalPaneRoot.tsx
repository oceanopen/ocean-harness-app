import { Box } from '@mui/material';
import { layoutFor, useTerminalPanesStore } from '@src/state/terminalPanes';
import PaneLayout from './PaneLayout';
import { paneScopeId } from './types';

interface TerminalPaneRootProps {
  // 容器 id（锚首段：任务页 task.id / 工作空间页 workspace.id）。
  containerId: string;
  // 当前工作空间的工作区目录（null = 解析中），透传至各 pane 终端的 cwd 派生。
  workspaceDir: string | null;
  // 终端直启值（'claude' 等命令串 | null；null = 解析中或不自动启动），透传至各 pane 的 directCommand。
  startupCli: string | null;
  // cwd 直用覆盖（T5.3 会话维度直连），透传至各 pane。
  cwdOverride?: string | null;
  // 会话 id（锚中段：全体 pane 共享——同会话分屏同锚前缀；null = legacy 占位段）。
  conversationId?: string | null;
  // 终端锚恢复 id（T2.2，仅主 pane 消费）。
  resumeClaudeId?: string | null;
}

/**
 * TerminalPaneRoot：终端区 split 树容器（docs/terminal_02_split_panes.md §3.4）。
 * 职责：读 store 渲染该 scope（容器+会话）的布局树。分割与关闭 pane 均由各 pane 内 TerminalView
 * 工具栏的按钮承担（EmbeddedTerminal props 闭包自识别目标 pane）：分割 = handleSplit（splitPane，左右/上下分屏）；关闭 = handleClose。
 */
export default function TerminalPaneRoot({ containerId, workspaceDir, startupCli, cwdOverride, conversationId, resumeClaudeId }: TerminalPaneRootProps) {
  const scopeId = paneScopeId(containerId, conversationId);
  const layout = useTerminalPanesStore(s => layoutFor(s.layouts[scopeId]));

  return (
    <Box sx={{ height: '100%', overflow: 'hidden' }}>
      <PaneLayout containerId={containerId} scopeId={scopeId} node={layout} workspaceDir={workspaceDir} startupCli={startupCli} cwdOverride={cwdOverride} conversationId={conversationId} resumeClaudeId={resumeClaudeId} />
    </Box>
  );
}
