// issue 状态固定常量（扁平模型：无 state_group/子状态层级、无 state_id、无 sortorder）。
// 固定 5 个状态，顺序即数组序（BACKLOG→TODO→IN_PROGRESS→DONE→CANCELLED）。
// 与后端 enums.StateCatalog 双端常量对齐（无 catalog 接口）；name 为中文硬编码（不走 i18n，数据层不国际化）。
export type StateCode = 'BACKLOG' | 'TODO' | 'IN_PROGRESS' | 'DONE' | 'CANCELLED';

// 状态展示元数据（列表分组头/徽章/看板列头用）。
export interface StateMeta {
  code: StateCode;
  name: string;
  color: string;
}

// 全部 5 个状态的固定展示元数据，数组序即固定顺序。
export const STATE_CATALOG: StateMeta[] = [
  { code: 'BACKLOG', name: '待办池', color: '#94a3b8' },
  { code: 'TODO', name: '待办', color: '#475569' },
  { code: 'IN_PROGRESS', name: '进行中', color: '#f59e0b' },
  { code: 'DONE', name: '已完成', color: '#16a34a' },
  { code: 'CANCELLED', name: '已取消', color: '#ef4444' },
];

// code → 元数据映射（徽章/列头按 code 直接查）。
export const STATE_MAP: Map<StateCode, StateMeta> = new Map(STATE_CATALOG.map(s => [s.code, s]));

// 状态固定顺序（列表分组/看板列序）。
export const STATE_ORDER: StateCode[] = STATE_CATALOG.map(s => s.code);

// 新建 issue 的默认状态。
export const STATE_CODE_DEFAULT: StateCode = 'BACKLOG';
