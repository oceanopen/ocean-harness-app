// split 分割窗口布局树类型（docs/terminal_02_split_panes.md §3.2）。
//
// 二叉树（orca 同构）：递归 split 只在 leaf 上操作，操作集收敛为
// splitPane / closePane，无重排/移动等复杂树变换。
//
// 类型同时被组件目录（PaneLayout/TerminalPaneRoot）与 state 域
// （terminalPanes store/actions）引用，是两者的共享契约。

export type SplitDirection = 'horizontal' | 'vertical';

/// divider 拖拽的 ratio 上下限（first 子占比 clamp 区间，文档 §3.2）。
export const MIN_PANE_RATIO = 0.2;
export const MAX_PANE_RATIO = 0.8;

export type PaneLayoutNode
  // 叶子 = 一个终端 pane。paneId: 'main'（主 pane，锚点即 issueId）| 8 位 uuid（附加 pane）
  = | { type: 'leaf'; paneId: string }
  // 分割节点：direction 决定 flex 方向；ratio ∈ [MIN, MAX] 为 first 子占比（divider
  // 拖拽 setRatio 按 id 定位修改）；children 恒二元（split 只作用于 leaf）。
  // id：split 生成时的 8 位随机标识——divider 拖拽定位目标节点用（树结构变化时
  // 按 path 定位脆弱，按 id 稳定）。
    | { type: 'split'; id: string; direction: SplitDirection; ratio: number; children: [PaneLayoutNode, PaneLayoutNode] };

/// 主 pane id（布局树的恒存根：初始布局 = 单 main leaf，关最后一个 pane 被拦截，
/// 保证终端区至少一个 pane——§3.5）。
export const MAIN_PANE_ID = 'main';

/// 生成附加 pane id：crypto.randomUUID() 前 8 位（orca 同长度；展示可辨识、
/// localStorage key 短）。
export function newPaneId(): string {
  return crypto.randomUUID().slice(0, 8);
}

/// 生成 split 节点 id：与 paneId 同规格（8 位随机）。
export function newSplitId(): string {
  return crypto.randomUUID().slice(0, 8);
}

/// 初始布局：单 main leaf（无分割）。
///
/// 注意：返回**共享常量引用**而非每次新建——zustand v5 selector 结果按 Object.is
/// 严格比较，新建对象会导致快照引用不稳 → useSyncExternalStore 判定 store 持续
/// 变化 → 无限重渲染（Maximum update depth exceeded，实测教训）。共享安全性由
/// 树操作的不可变风格保证：splitNode/closeNode 从不修改入参节点，只会新建分支，
/// 该常量在渲染路径上恒为只读。
export const INITIAL_LAYOUT: PaneLayoutNode = { type: 'leaf', paneId: MAIN_PANE_ID };

/// initialLayout：INITIAL_LAYOUT 的访问函数（语义化入口，保持调用方书写习惯）。
export function initialLayout(): PaneLayoutNode {
  return INITIAL_LAYOUT;
}
