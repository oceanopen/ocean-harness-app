/**
 * terminalPanes 域 client 状态：各 scope（`${容器id}::${会话id}`，见 types.ts paneScopeId）的 split 布局树（docs/terminal_02_split_panes.md §3.3）。纯前端 UI 状态——PTY 会话生命周期完全在后端（锚点 sessionId），布局丢失/损坏的最坏情况 = 回落单 main pane + 重新挂载即 reattach，不丢会话（§5.1）。
 * 持久化：localStorage 按 scope（loadLayout/saveLayout）。
 * hydration 由消费方渲染期调 ensureLayout（store 无记录时同步写入读回值，F5 首渲染即还原布局）；模块级 subscribe 监听 layouts 引用变化逐 scope 落盘。
 */

import type { PaneLayoutNode, SplitDirection } from '@src/windows/panel/DevWorkbenchPage/components/TerminalPanes/types';
import { create } from 'zustand';
import { closeNode, layoutFor, loadLayout, saveLayout, setRatioNode, splitNode } from './actions';

interface TerminalPanesState {
  /**
   * scopeId → 布局树。未登记 scope 由 selector 惰性派生单 main leaf（不预填）；
   * 打开过的 scope 经 ensureLayout hydration 后有记录（持久化还原）。
   */
  layouts: Record<string, PaneLayoutNode>;
  splitPane: (scopeId: string, paneId: string, direction: SplitDirection) => void;
  closePane: (scopeId: string, paneId: string) => void;
  setRatio: (scopeId: string, splitId: string, ratio: number) => void;
}

export const useTerminalPanesStore = create<TerminalPanesState>()(set => ({
  layouts: {},
  splitPane: (scopeId, paneId, direction) => set((state) => {
    const tree = splitNode(layoutFor(state.layouts[scopeId]), paneId, direction);
    return { layouts: { ...state.layouts, [scopeId]: tree } };
  }),
  closePane: (scopeId, paneId) => set((state) => {
    // main pane 不可关（§3.5：终端区至少一个 pane；「关闭终端」语义 = 回单 main）。
    if (paneId === 'main') {
      return state;
    }
    const tree = closeNode(layoutFor(state.layouts[scopeId]), paneId);
    return { layouts: { ...state.layouts, [scopeId]: tree } };
  }),
  setRatio: (scopeId, splitId, ratio) => set((state) => {
    const tree = setRatioNode(layoutFor(state.layouts[scopeId]), splitId, ratio);
    return { layouts: { ...state.layouts, [scopeId]: tree } };
  }),
}));

/**
 * hydration：store 无该 scope 记录时从 localStorage 读回写入（渲染期调用，同步）；幂等——已有记录（本会话操作过）原样保留，返回该 scope 的有效布局。
 * 同 INITIAL_LAYOUT 引用约定：读回值落 store 后 selector 走 store 引用，
 * 不在渲染路径新建对象（zustand v5 selector 引用稳定契约，见 types.ts 注释）。
 */
export function ensureLayout(scopeId: string): PaneLayoutNode {
  const state = useTerminalPanesStore.getState();
  if (state.layouts[scopeId] != null) {
    return state.layouts[scopeId];
  }
  const restored = loadLayout(scopeId);
  useTerminalPanesStore.setState(s => ({ layouts: { ...s.layouts, [scopeId]: restored } }));
  return restored;
}

/**
 * 持久化订阅：layouts 引用变化 → 逐 scope 比较落盘（仅变更项；单 main 树移除 key）。
 * 模块级一次性注册（多窗口各自 JS realm 独立，无重复订阅问题）。
 */
let lastPersisted: Record<string, PaneLayoutNode> = {};
useTerminalPanesStore.subscribe((state) => {
  for (const [scopeId, tree] of Object.entries(state.layouts)) {
    if (lastPersisted[scopeId] !== tree) {
      saveLayout(scopeId, tree);
    }
  }
  lastPersisted = state.layouts;
});
