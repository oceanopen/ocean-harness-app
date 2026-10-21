/**
 * tracker 域 query key 工厂（SSOT）。
 * 末位用对象 { workspaceId }：比裸 id 可读、可扩展。root 根用于整域失效。
 */
export const trackerKeys = {
  root: ['tracker'] as const,
  workspaces: () => [...trackerKeys.root, 'workspaces'] as const,
  workspaceTypes: (workspaceId: string) => [...trackerKeys.root, 'workspaceTypes', { workspaceId }] as const,
  workspaceIssues: (workspaceId: string) => [...trackerKeys.root, 'workspaceIssues', { workspaceId }] as const,
} as const;
