import type { Priority } from '@src/services';

// 优先级元数据（跨页面共享单一来源，src/components 常量层）：顺序、颜色 token、业务排序权重。
// 项目事项管理列表分组排序、IssueCard 徽标、PrioritySelect 下拉均从此引用，避免多处分散维护
// 导致漏改。原属 TrackerPage 页面目录，因 IssueCard/PrioritySelect（src/components 共享组件）
// 反向依赖页面目录而迁出收口（2026-09-24）。

// 下拉/展示顺序（urgent 在前）。
export const PRIORITY_ORDER: Priority[] = ['urgent', 'high', 'medium', 'low', 'none'];

// 色点颜色 token（与 MUI palette 对齐）。
export const PRIORITY_COLOR: Record<Priority, string> = {
  urgent: 'error.main',
  high: 'warning.main',
  medium: 'info.main',
  low: 'text.secondary',
  none: 'text.disabled',
};

// 业务权重（升序）——后端 orderBy=priority 为文本字典序不可靠，前端按 weight 重排。
export const PRIORITY_WEIGHT: Record<Priority, number> = {
  urgent: 0,
  high: 1,
  medium: 2,
  low: 3,
  none: 4,
};
