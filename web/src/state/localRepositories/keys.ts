// localRepositories 域 query key 工厂（SSOT）。
// 本地仓库为全局列表（无 workspace/project 维度），单一 list key 即可；
// root 用于整域失效；detail 预留 getInfo 精确缓存（本期页面未用）。
export const localRepositoryKeys = {
  root: ['localRepositories'] as const,
  list: () => [...localRepositoryKeys.root, 'list'] as const,
  detail: (id: number) => [...localRepositoryKeys.root, 'detail', { id }] as const,
  // 仓库的本地分支列表（git branch 默认仅本地分支）；远程分支后续按需扩展（remoteBranches）。
  localBranches: (id: number) => [...localRepositoryKeys.root, 'localBranches', { id }] as const,
} as const;
