// agentCatalog 域 query key 工厂（SSOT）。root 根用于整域失效。
export const agentCatalogKeys = {
  root: ['agentCatalog'] as const,
  list: () => [...agentCatalogKeys.root, 'list'] as const,
} as const;
