// terminalPanes 域 client 状态：各 issue 的 split 布局树（docs/terminal_02_split_panes.md §3.3）。
//
// 纯前端 UI 状态——PTY 会话生命周期完全在后端（锚点 sessionId），布局丢失/损坏
// 的最坏情况 = 回落单 main pane + 重新挂载即 reattach，不丢会话（§5.1）。
// 持久化（localStorage 按 issue）任务 3 接入；本期内存态。

import type { PaneLayoutNode, SplitDirection } from '@src/windows/panel/DevWorkbenchPage/components/TerminalPanes/types';
import { create } from 'zustand';
import { closeNode, layoutFor, splitNode } from './actions';

interface TerminalPanesState {
  /// issueId → 布局树。未登记 issue 由 selector 惰性派生单 main leaf（不预填）。
  layouts: Record<string, PaneLayoutNode>;
  /// issueId → 各 issue 独立的活跃 pane（focus 跟随，任务 4 接入 xterm focus 事件；
  /// 本期分割/关闭作用对象兜底 = 树上最后一个 leaf）。
  activePanes: Record<string, string>;
  splitPane: (issueId: string, paneId: string, direction: SplitDirection) => void;
  closePane: (issueId: string, paneId: string) => void;
  setActivePane: (issueId: string, paneId: string) => void;
}

export const useTerminalPanesStore = create<TerminalPanesState>()(set => ({
  layouts: {},
  activePanes: {},
  splitPane: (issueId, paneId, direction) => set((state) => {
    const tree = splitNode(layoutFor(state.layouts[issueId]), paneId, direction);
    return { layouts: { ...state.layouts, [issueId]: tree } };
  }),
  closePane: (issueId, paneId) => set((state) => {
    // main pane 不可关（§3.5：终端区至少一个 pane；「关闭终端」语义 = 回单 main）。
    if (paneId === 'main') {
      return state;
    }
    const tree = closeNode(layoutFor(state.layouts[issueId]), paneId);
    return { layouts: { ...state.layouts, [issueId]: tree } };
  }),
  setActivePane: (issueId, paneId) => set(state => ({
    activePanes: { ...state.activePanes, [issueId]: paneId },
  })),
}));
