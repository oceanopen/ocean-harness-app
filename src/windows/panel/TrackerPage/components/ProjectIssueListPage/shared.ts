// Issue 列表/看板/卡片共用的非组件常量与类型（独立成文件，避免页面导出非组件触发 react-refresh 规则）。

// 父 issue 的子任务统计（done/total），列表行/看板卡/统一卡片的进度小标共用。
export type SubtaskStats = Map<number, { done: number; total: number }>;

// 单行文本截断样式，供卡片标题/列名/标签等复用。
export const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;
