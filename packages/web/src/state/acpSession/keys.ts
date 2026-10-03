// acpSession 域 query key 工厂（SSOT）。root 根用于整域失效；view 按 issue 隔离
// （SSE 帧归约与 getInfo 读端共用同一 key）。
export const acpSessionKeys = {
  root: ['acpSession'] as const,
  view: (issueId: string) => [...acpSessionKeys.root, 'view', { issueId }] as const,
} as const;
