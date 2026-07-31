// 单行文本截断样式（与 IssueListPage 列表行一致），供看板卡片标题/列名复用。
// 独立成文件避免 IssueListPage 导出非组件常量触发 react-refresh 规则。
export const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;
