// split 布局树纯函数操作（docs/terminal_02_split_panes.md §3.3）。
// 树变换均为不可变风格（返回新树，不改入参），由 store action 包装落库。
// 持久化（loadLayout/saveLayout）：localStorage 按 issue 存 JSON，损坏/缺失回落
// 单 main leaf——PTY 会话在后端常驻，布局丢失仅 UI 回落 + reattach，不伤会话（§5.1）。

import type { PaneLayoutNode, SplitDirection } from '@src/windows/panel/DevWorkbenchPage/components/TerminalPanes/types';
import {
  INITIAL_LAYOUT,
  MAIN_PANE_ID,
  MAX_PANE_RATIO,
  MIN_PANE_RATIO,
  newPaneId,
  newSplitId,
} from '@src/windows/panel/DevWorkbenchPage/components/TerminalPanes/types';

/// 在 paneId 对应 leaf 上分割：leaf → split(0.5)，first = 原 pane，second = 新 pane
/// （新 pane 出现在分割方向的后位——右/下）。paneId 不存在时返回原树（幂等防御）。
export function splitNode(tree: PaneLayoutNode, paneId: string, direction: SplitDirection): PaneLayoutNode {
  if (tree.type === 'leaf') {
    if (tree.paneId !== paneId) {
      return tree;
    }
    return {
      type: 'split',
      id: newSplitId(),
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

/// 调整 splitId 对应节点的 ratio（divider 拖拽）：clamp 到 [MIN, MAX]；未命中原树返回。
export function setRatioNode(tree: PaneLayoutNode, splitId: string, ratio: number): PaneLayoutNode {
  if (tree.type === 'leaf') {
    return tree;
  }
  const clamped = Math.max(MIN_PANE_RATIO, Math.min(MAX_PANE_RATIO, ratio));
  if (tree.id === splitId) {
    return tree.ratio === clamped ? tree : { ...tree, ratio: clamped };
  }
  const [first, second] = tree.children;
  const nextFirst = setRatioNode(first, splitId, ratio);
  const nextSecond = setRatioNode(second, splitId, ratio);
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
  return tree ?? INITIAL_LAYOUT;
}

// ---------- 持久化（localStorage 按 issue，文档 §3.3） ----------

/// localStorage key：terminal_pane_layout_<issueId>（paneId 8 位设计保证 key 短）。
function layoutKey(issueId: string): string {
  return `terminal_pane_layout_${issueId}`;
}

/// 递归结构校验：防手改/版本演进/截断产生的脏数据进渲染层。
/// 规则：leaf 有 paneId 字符串；split 有 id/direction 合法/ratio 有限数值在区间内/
/// children 长度 2。任何一处不满足 → 整树判废（回落单 main，§5.1 布局丢失不伤会话）。
function isValidNode(node: unknown): node is PaneLayoutNode {
  if (typeof node !== 'object' || node == null) {
    return false;
  }
  const n = node as Record<string, unknown>;
  if (n.type === 'leaf') {
    return typeof n.paneId === 'string' && n.paneId.length > 0;
  }
  if (n.type === 'split') {
    return typeof n.id === 'string'
      && (n.direction === 'horizontal' || n.direction === 'vertical')
      && typeof n.ratio === 'number' && Number.isFinite(n.ratio)
      && n.ratio >= MIN_PANE_RATIO && n.ratio <= MAX_PANE_RATIO
      && Array.isArray(n.children) && n.children.length === 2
      && isValidNode(n.children[0]) && isValidNode(n.children[1]);
  }
  return false;
}

/// 读回 issue 布局：无记录/JSON 解析失败/结构校验失败 → INITIAL_LAYOUT（单 main）。
export function loadLayout(issueId: string): PaneLayoutNode {
  try {
    const raw = localStorage.getItem(layoutKey(issueId));
    if (raw == null) {
      return INITIAL_LAYOUT;
    }
    const parsed: unknown = JSON.parse(raw);
    return isValidNode(parsed) ? parsed : INITIAL_LAYOUT;
  } catch {
    // JSON.parse 抛错（截断/非法）等同判废，不区分。
    return INITIAL_LAYOUT;
  }
}

/// 写入 issue 布局：非默认树（有分割）才落盘；单 main 树移除 key——localStorage
/// 不积 corpse，也保证「关闭全部附加 pane」后下次打开回到干净单 pane。
export function saveLayout(issueId: string, tree: PaneLayoutNode): void {
  try {
    if (tree.type === 'leaf') {
      localStorage.removeItem(layoutKey(issueId));
      return;
    }
    localStorage.setItem(layoutKey(issueId), JSON.stringify(tree));
  } catch (e) {
    // 写失败（隐私模式/超限）不致命：布局仅 UI 状态，下次会话回落单 pane。
    console.warn('[terminalPanes] save layout failed:', e);
  }
}

/// main pane 判定（锚点派生用：main 锚点 = 裸 issueId，见 EmbeddedTerminal）。
export function isMainPane(paneId: string): boolean {
  return paneId === MAIN_PANE_ID;
}
