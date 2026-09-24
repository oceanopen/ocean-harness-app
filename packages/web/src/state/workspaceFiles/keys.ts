// workspaceFiles 域 query key 工厂（SSOT）。
// 树按 issue 维度缓存；内容按 issue+path（切 tab 即切 key，回切命中缓存）。
// Git 变更同款：变更列表按 issue、单文件 diff 按 issue+path。
// baseDir 不入 key——与 issueWorkspaceKeys 同款取舍（baseDir 变更是罕见运维动作，接受缓存惯性）。
export const workspaceFilesKeys = {
  root: ['workspaceFiles'] as const,
  tree: (issueId: string) => [...workspaceFilesKeys.root, 'tree', { issueId }] as const,
  content: (issueId: string, path: string) => [...workspaceFilesKeys.root, 'content', { issueId, path }] as const,
  gitChanges: (issueId: string) => [...workspaceFilesKeys.root, 'gitChanges', { issueId }] as const,
  // fileDiff 的 issueId 与 path 分段（不并成一个对象）：面板刷新需要按 issue 前缀失效该
  // issue 的全部 diff 缓存（[...root, 'fileDiff', { issueId }] 结构化前缀匹配）。
  fileDiff: (issueId: string, path: string) => [...workspaceFilesKeys.root, 'fileDiff', { issueId }, path] as const,
} as const;
