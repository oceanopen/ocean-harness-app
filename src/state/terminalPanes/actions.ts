// split 布局树纯函数操作（docs/terminal_02_split_panes.md §3.3）。
// 树变换均为不可变风格（返回新树，不改入参），由 store action 包装落库。

import type { PaneLayoutNode, SplitDirection } from '@src/windows/panel/DevWorkbenchPage/components/TerminalPanes/types';
import { initialLayout, MAIN_PANE_ID, newPaneId } from '@src/windows/panel/DevWorkbenchPage/components/TerminalPanes/types';

/// 在 paneId 对应 leaf 上分割：leaf → split(0.5)，first = 原 pane，second = 新 pane
/// （新 pane 出现在分割方向的后位——右/下）。paneId 不存在时返回原树（幂等防御）。
export function splitNode(tree: PaneLayoutNode, paneId: string, direction: SplitDirection): PaneLayoutNode {
  if (tree.type === 'leaf') {
    if (tree.paneId !== paneId) {
      return tree;
    }
    return {
      type: 'split',
      direction,
      ratio: 0.5,
      children: [{ type: 'leaf', paneId }, { type: 'leaf', paneId: newPaneId() }],
    };
  }
  const [first, second] = tree.children;
  return { ...tree, children: [splitNode(first, paneId, direction), splitNode(second, paneId, direction)] };
}

/// 关闭 paneId 对应 leaf：从树上剪枝；父 split 只剩单子时折叠（单子提升替代父节点）。
/// paneId 不存在 / 是 main（不可关，§3.5 由调用方拦截，此处兜底原样返回）时返回原树。
export function closeNode(tree: PaneLayoutNode, paneId: string): PaneLayoutNode {
  // main 不可关（调用方已拦截；此处兜底——main 恒为根 leaf 或嵌套 leaf，一律原样返回）。
  if (tree.type === 'leaf') {
    return tree;
  }
  const next = pruneLeaf(tree, paneId);
  // 目标 leaf 不在树上（如并发关闭后重放）：原样返回。
  return next ?? tree;
}

/// 递归剪枝：命中 paneId 的 leaf 被剪除（返回 null 表示子树整体移除）；父 split
/// 单子折叠——null 侧被另一侧整体提升替代。未命中的子树原样引用返回（引用相等，
/// 调用方可据此免重建）。main leaf 不会被命中（closeNode 入口语义，见上）。
function pruneLeaf(tree: PaneLayoutNode, paneId: string): PaneLayoutNode | null {
  if (tree.type === 'leaf') {
    return tree.paneId === paneId ? null : tree;
  }
  const [first, second] = tree.children;
  // main 恒在 first 位（splitNode 原 pane 在 first），此判定非必需——main 由
  // 调用方/store 双层拦截，此处不重复。
  const nextFirst = pruneLeaf(first, paneId);
  const nextSecond = pruneLeaf(second, paneId);
  if (nextFirst == null) {
    // first 侧剪除：second 整体提升（保留其内部结构与比例）。
    return nextSecond;
  }
  if (nextSecond == null) {
    return nextFirst;
  }
  if (nextFirst === first && nextSecond === second) {
    return tree;
  }
  return { ...tree, children: [nextFirst, nextSecond] };
}

/// 树上全部 leaf paneId（从左到右/从上到下）。活跃 pane 兜底与工具条判定用。
export function leafPaneIds(tree: PaneLayoutNode): string[] {
  if (tree.type === 'leaf') {
    return [tree.paneId];
  }
  return [...leafPaneIds(tree.children[0]), ...leafPaneIds(tree.children[1])];
}

/// 树上是否存在 paneId（main 恒在）。
export function hasPane(tree: PaneLayoutNode, paneId: string): boolean {
  return leafPaneIds(tree).includes(paneId);
}

/// issue 的有效布局：store 未登记（首次打开/布局损坏回落）→ 单 main leaf。
/// selector 侧调用（store 不预填所有 issue，惰性派生）。
export function layoutFor(tree: PaneLayoutNode | undefined): PaneLayoutNode {
  return tree ?? initialLayout();
}

/// main pane 判定（锚点派生用：main 锚点 = 裸 issueId，见 EmbeddedTerminal）。
export function isMainPane(paneId: string): boolean {
  return paneId === MAIN_PANE_ID;
}
