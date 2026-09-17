// issueWorkspace 域 query key 工厂（SSOT）。
// 状态随 issue 维度缓存（切 issue 即切 key，轮询互不串扰）；root 用于整域失效。
export const issueWorkspaceKeys = {
  root: ['issueWorkspace'] as const,
  status: (issueId: string) => [...issueWorkspaceKeys.root, 'status', { issueId }] as const,
} as const;
