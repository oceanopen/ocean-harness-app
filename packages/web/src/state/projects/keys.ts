/**
 * projects 域 query key 工厂（SSOT）。root 根用于整域失效。
 */
export const projectsKeys = {
  root: ['projects'] as const,
  projects: () => [...projectsKeys.root, 'projects'] as const,
} as const;
