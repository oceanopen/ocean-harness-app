// tracker 域 query key 工厂（SSOT）。
// 末位用对象 { workspaceId } / { projectId }：比裸 id 可读、可扩展（将来加筛选参数往对象加键）。
// root 根用于整域失效（调试、将来接 Tauri tracker:changed 事件时一次性 invalidate）。
export const trackerKeys = {
  root: ['tracker'] as const,
  workspaces: () => [...trackerKeys.root, 'workspaces'] as const,
  workspaceProjects: (workspaceId: number) => [...trackerKeys.root, 'workspaceProjects', { workspaceId }] as const,
  projectIssues: (projectId: number) => [...trackerKeys.root, 'projectIssues', { projectId }] as const,
  projectStates: (projectId: number) => [...trackerKeys.root, 'projectStates', { projectId }] as const,
} as const;
