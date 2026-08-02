// 父 issue 的子任务统计（done/total），列表行/看板卡的进度小标共用。
export type SubtaskStats = Map<number, { done: number; total: number }>;

// 单行文本截断样式（与 ProjectIssueListPage 列表行一致），供看板卡片标题/列名复用。
// 独立成文件避免 ProjectIssueListPage 导出非组件常量触发 react-refresh 规则。
export const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;
