// imBots 域 query key 工厂（SSOT）。root 根用于整域失效。
export const imBotsKeys = {
  root: ['imBots'] as const,
  list: () => [...imBotsKeys.root, 'list'] as const,
} as const;
