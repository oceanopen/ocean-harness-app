// imBots 域 query key 工厂（SSOT）。root 根用于整域失效（列表 + 全部会话 key）。
export const imBotsKeys = {
  root: ['imBots'] as const,
  list: () => [...imBotsKeys.root, 'list'] as const,
  // 会话列表按 bot 维度缓存（T3.1 会话级绑定面；key 末位对象式参数，见 state/README.md）。
  conversations: (botId: number) => [...imBotsKeys.root, 'conversations', { botId }] as const,
} as const;
