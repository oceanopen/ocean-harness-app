// workspaceFiles 域 query key 工厂（SSOT）。
// 树按 issue 维度缓存；内容按 issue+path（切 tab 即切 key，回切命中缓存）。
// baseDir 不入 key——与 issueWorkspaceKeys 同款取舍（baseDir 变更是罕见运维动作，接受缓存惯性）。
export const workspaceFilesKeys = {
  root: ['workspaceFiles'] as const,
  tree: (issueId: string) => [...workspaceFilesKeys.root, 'tree', { issueId }] as const,
  content: (issueId: string, path: string) => [...workspaceFilesKeys.root, 'content', { issueId, path }] as const,
} as const;
