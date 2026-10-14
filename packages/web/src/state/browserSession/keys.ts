/**
 * browserSession 域 query key 工厂（SSOT）。会话后端全局单源（全部 profile 同一快照、
 * 同一 SSE 流），无 issue 维度；root 用于整域失效；view = 全量快照缓存（SSE 帧归约与
 * getInfo 读端共用同一 key）。实时画面帧流不走 query 缓存（keep-latest 高频帧，见 useBrowserFrameStream）。
 */
const ROOT = ['browserSession'] as const;

export const browserSessionKeys = {
  root: ROOT,
  view: [...ROOT, 'view'] as const,
} as const;
