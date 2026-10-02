// doctor 域 query key 工厂（SSOT）。root 根用于整域失效。
export const doctorKeys = {
  root: ['doctor'] as const,
  reports: () => [...doctorKeys.root, 'reports'] as const,
} as const;
