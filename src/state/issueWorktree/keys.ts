// issueWorktree 域 query key 工厂（SSOT）。
// worktree 元数据按 issueId 查（getList）；末位对象 { issueId } 便于扩展。
// root 用于整域失效（调试、将来接 worktree 变更事件时一次性 invalidate）。
export const issueWorktreeKeys = {
  root: ['issueWorktree'] as const,
  list: (issueId: number) => [...issueWorktreeKeys.root, 'list', { issueId }] as const,
} as const;
