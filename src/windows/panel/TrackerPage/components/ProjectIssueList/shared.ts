// Issue 列表/看板/卡片共用的非组件常量与类型（独立成文件，避免页面导出非组件触发 react-refresh 规则）。
// 状态顺序/元数据统一从 @src/state/tracker 的 STATE_ORDER/STATE_MAP 取（双端固定常量）。

// 父 issue 的子任务统计（done/total），列表行/看板卡/统一卡片的进度小标共用。
export type SubtaskStats = Map<number, { done: number; total: number }>;

// 展开/收起 icon 列宽（无子项时留白占位，保证左侧对齐）。IssueCard 与 StateGroupCard 共用。
export const GUTTER_WIDTH = 28;

// 单行文本截断样式，供卡片标题/列名/标签等复用。
export const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;
