// devWorkbench 域 query key 工厂（SSOT）。
// 本期数据全部派生自 tracker 缓存（useWorkspaces/useWorkspaceProjects/useProjectIssues/useProjectStateViews），
// 无独立 HTTP 查询；root 预留给将来纯 devWorkbench 查询（如 worktree 记录）做整域失效。
export const devWorkbenchKeys = {
  root: ['devWorkbench'] as const,
} as const;
