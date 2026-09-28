// pluginMarketplace 域 query key 工厂（SSOT）。
// 插件市场为全局列表（无 workspace/project 维度，SSOT 恒为 claude 侧），单一 list key 即可；
// root 用于整域失效。
export const pluginMarketplaceKeys = {
  root: ['pluginMarketplaces'] as const,
  list: () => [...pluginMarketplaceKeys.root, 'list'] as const,
} as const;
