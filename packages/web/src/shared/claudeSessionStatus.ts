import type { ClaudeSessionInfo, ClaudeSessionStatus } from './bindings';

// 状态色规范 SSOT
// Dead 用深灰而非警示红：无会话休眠非错误，比 Idle 深一档区分。
// GitPending 用 info 蓝：提示性待办（空闲但有未提交改动），与绿/橙/灰/深灰在色相轮上最大区分。
export const CLAUDE_SESSION_STATUS_COLOR: Record<ClaudeSessionStatus, string> = {
  Busy: '#4caf50', // success 绿（工作中）
  Waiting: '#ff9800', // warning 橙（提醒用户）
  GitPending: '#2196f3', // info 蓝（空闲且有未提交改动，待关注）
  Idle: '#9e9e9e', // 灰（空闲）
  Dead: '#616161', // 深灰（休眠）
};

// status → i18n key 映射 SSOT。panel 卡片与 pet_task 列表项共用，
// 新增状态时 Record 的 exhaustive 检查会强制同步，避免漏改一处导致 i18n key 裸露。
export const CLAUDE_SESSION_STATUS_I18N_KEY: Record<ClaudeSessionStatus, string> = {
  Busy: 'claudeSessions:status.busy',
  Waiting: 'claudeSessions:status.waiting',
  GitPending: 'claudeSessions:status.gitPending',
  Idle: 'claudeSessions:status.idle',
  Dead: 'claudeSessions:status.dead',
};

// 排序优先级 SSOT：Waiting > GitPending > Busy > Idle > Dead。数字越小优先级越高。
// panel 列表、pet_task 列表、桌宠聚合共用，避免多份副本靠注释"对齐"而在加状态时漏改其一。
export const CLAUDE_SESSION_STATUS_PRIORITY: Record<ClaudeSessionStatus, number> = {
  Waiting: 0,
  GitPending: 1,
  Busy: 2,
  Idle: 3,
  Dead: 4,
};

// 按优先级排序会话，同状态内按 updatedAt 倒序（最近活动在前）。
export function sortClaudeSessions(sessions: ClaudeSessionInfo[]): ClaudeSessionInfo[] {
  return [...sessions].sort((a, b) => {
    const pri = CLAUDE_SESSION_STATUS_PRIORITY[a.status] - CLAUDE_SESSION_STATUS_PRIORITY[b.status];
    if (pri !== 0) {
      return pri;
    }
    return b.updatedAt - a.updatedAt;
  });
}

// 待关注会话判定口径 SSOT，用于 panel summary 计数、pet_task 列表过滤、桌宠徽章数字、pet_task 显隐
// （Busy+Waiting+GitPending）。GitPending 纳入：用户 commit 后空闲会话仍需展示"待提交"，
// 且"仅 GitPending 无活跃"场景下 count>0 才能让 pet_task 可见。panel 与 pet_task 共用此口径。
export const CLAUDE_SESSION_ATTENTION_STATUSES: ReadonlySet<ClaudeSessionStatus> = new Set([
  'Busy',
  'Waiting',
  'GitPending',
]);

export function isAttentionClaudeSession(s: ClaudeSessionInfo): boolean {
  return CLAUDE_SESSION_ATTENTION_STATUSES.has(s.status);
}

export function countAttentionClaudeSessions(sessions: ClaudeSessionInfo[]): number {
  return sessions.filter(isAttentionClaudeSession).length;
}
