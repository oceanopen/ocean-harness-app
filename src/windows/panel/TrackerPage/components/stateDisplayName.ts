// 默认状态名 → 中文展示名映射（不考虑多语言：仅命中后端种子的 5 个默认状态名时显示中文；
// 用户改名或自建状态按原名返回）。供 ProjectStateSelect / 查询表单状态筛选 / 看板列头共用。
const DEFAULT_STATE_DISPLAY_NAME: Record<string, string> = {
  'Backlog': '待办池',
  'Todo': '未开始',
  'In Progress': '进行中',
  'Done': '已完成',
  'Cancelled': '已取消',
};

// stateDisplayName 返回状态的展示名：默认状态命中映射则给中文，否则原样返回 name。
export function stateDisplayName(name: string): string {
  return DEFAULT_STATE_DISPLAY_NAME[name] ?? name;
}
