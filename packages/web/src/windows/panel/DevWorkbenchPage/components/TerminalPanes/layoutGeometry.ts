// 分屏布局树 → 平铺几何（纯函数，docs/terminal_02_split_panes.md §3.4 平铺版）。
//
// 嵌套 flex 渲染在分屏时会因「同位置元素类型变化」触发 React 整树卸载重建存活
// pane → ring 回放进失配几何 + 校正 resize 交错 → 左右分屏顶部空白（实测教训）。
// 平铺渲染要求「组件结构恒定、布局树只产出几何」：本模块把布局二叉树展开为
// 一组 pane/divider 矩形（px、相对根容器），消费方按 key 平铺渲染。
//
// 独立模块而非组件文件导出：react-refresh/only-export-components 拦截组件文件
// 导出非组件（terminalTheme.ts 先例）。

import type { PaneLayoutNode, SplitDirection } from './types';

/// divider 厚度（px）：沿分割方向占据，两侧子区域按 ratio 划分扣除后的剩余空间。
export const PANE_DIVIDER_SIZE = 4;

/// leaf pane 矩形（px，相对根容器）。
export interface PaneRect {
  paneId: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

/// split 分隔条矩形 + 拖拽换算上下文。
export interface DividerRect {
  splitId: string;
  direction: SplitDirection;
  /// divider 自身矩形（px，相对根容器；厚度侧 = PANE_DIVIDER_SIZE，另一侧贯通所属 split）。
  x: number;
  y: number;
  width: number;
  height: number;
  /// 所属 split 的矩形与当前 ratio：拖拽换算的基准（指针位移 → 比例增量）。
  splitX: number;
  splitY: number;
  splitWidth: number;
  splitHeight: number;
  ratio: number;
}

export interface PaneLayoutGeometry {
  panes: PaneRect[];
  dividers: DividerRect[];
}

/// 内部递归用的矩形 accumulator 参数。
interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

/// 布局树 → 平铺几何。纯函数：给定（树, 根容器尺寸）确定全部 pane 与 divider
/// 的矩形；容器尺寸变化（窗口缩放/面板开合/比例调整）走同一条重算路径。
export function layoutGeometry(tree: PaneLayoutNode, width: number, height: number): PaneLayoutGeometry {
  const geometry: PaneLayoutGeometry = { panes: [], dividers: [] };
  layoutInto(tree, { x: 0, y: 0, width, height }, geometry);
  return geometry;
}

function layoutInto(tree: PaneLayoutNode, rect: Rect, out: PaneLayoutGeometry): void {
  if (tree.type === 'leaf') {
    out.panes.push({ paneId: tree.paneId, x: rect.x, y: rect.y, width: rect.width, height: rect.height });
    return;
  }
  const { direction, ratio, id, children } = tree;
  if (direction === 'horizontal') {
    // 左右分：divider 占宽度，first 子按 ratio 分剩余宽。
    const avail = Math.max(0, rect.width - PANE_DIVIDER_SIZE);
    const first = ratio * avail;
    layoutInto(children[0], { x: rect.x, y: rect.y, width: first, height: rect.height }, out);
    out.dividers.push({
      splitId: id,
      direction,
      x: rect.x + first,
      y: rect.y,
      width: PANE_DIVIDER_SIZE,
      height: rect.height,
      splitX: rect.x,
      splitY: rect.y,
      splitWidth: rect.width,
      splitHeight: rect.height,
      ratio,
    });
    layoutInto(children[1], { x: rect.x + first + PANE_DIVIDER_SIZE, y: rect.y, width: avail - first, height: rect.height }, out);
    return;
  }
  // 上下分：divider 占高度，first 子按 ratio 分剩余高。
  const avail = Math.max(0, rect.height - PANE_DIVIDER_SIZE);
  const first = ratio * avail;
  layoutInto(children[0], { x: rect.x, y: rect.y, width: rect.width, height: first }, out);
  out.dividers.push({
    splitId: id,
    direction,
    x: rect.x,
    y: rect.y + first,
    width: rect.width,
    height: PANE_DIVIDER_SIZE,
    splitX: rect.x,
    splitY: rect.y,
    splitWidth: rect.width,
    splitHeight: rect.height,
    ratio,
  });
  layoutInto(children[1], { x: rect.x, y: rect.y + first + PANE_DIVIDER_SIZE, width: rect.width, height: avail - first }, out);
}
