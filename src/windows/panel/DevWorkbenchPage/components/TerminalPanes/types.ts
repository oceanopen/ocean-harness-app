// split 分割窗口布局树类型（docs/terminal_02_split_panes.md §3.2）。
//
// 二叉树（orca 同构）：递归 split 只在 leaf 上操作，操作集收敛为
// splitPane / closePane，无重排/移动等复杂树变换。
//
// 类型同时被组件目录（PaneLayout/TerminalPaneRoot）与 state 域
// （terminalPanes store/actions）引用，是两者的共享契约。

export type SplitDirection = 'horizontal' | 'vertical';

export type PaneLayoutNode
  // 叶子 = 一个终端 pane。paneId: 'main'（主 pane，锚点即 issueId）| 8 位 uuid（附加 pane）
  = | { type: 'leaf'; paneId: string }
  // 分割节点：direction 决定 flex 方向；ratio ∈ (0.1, 0.9) 为 first 子占比
  // （divider 拖拽 clamp，任务 3 接入）；children 恒二元（split 只作用于 leaf）。
    | { type: 'split'; direction: SplitDirection; ratio: number; children: [PaneLayoutNode, PaneLayoutNode] };

/// 主 pane id（布局树的恒存根：初始布局 = 单 main leaf，关最后一个 pane 被拦截，
/// 保证终端区至少一个 pane——§3.5）。
export const MAIN_PANE_ID = 'main';

/// 生成附加 pane id：crypto.randomUUID() 前 8 位（orca 同长度；展示可辨识、
/// localStorage key 短——任务 3 持久化）。
export function newPaneId(): string {
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
