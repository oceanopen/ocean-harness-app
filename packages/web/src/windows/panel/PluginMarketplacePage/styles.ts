// 插件市场 feature 内共享的文本截断样式（市场卡片与插件抽屉复用，避免两处各写一份漂移）。

// 单行截断（名称/路径类）。
export const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

// 2 行截断（描述类文本）。
export const clamp2Sx = {
  display: '-webkit-box',
  WebkitBoxOrient: 'vertical',
  WebkitLineClamp: 2,
  overflow: 'hidden',
} as const;
