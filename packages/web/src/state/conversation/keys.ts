/**
 * conversation 域 query key 工厂（SSOT）。
 * 末位用对象 { ownerType, ownerId }：比裸 id 可读、可扩展。root 根用于整域失效。
 */
export const conversationKeys = {
  root: ['conversation'] as const,
  conversations: (ownerType: string, ownerId: string) =>
    [...conversationKeys.root, 'conversations', { ownerType, ownerId }] as const,
} as const;
