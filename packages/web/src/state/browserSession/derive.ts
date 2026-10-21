import type { BrowserSessionStatus } from '@src/services';

/** 单任务会话槽位数（与后端 browser.MaxSessionsPerTask 对齐：基名 + -2..-5 后缀）。 */
export const TASK_SESSION_SLOT_COUNT = 5;

/**
 * 任务派生 profile 名（任务↔会话关联 SSOT）：`issue-<issueId>` 命名约定，面板过滤、
 * 「+」新建与 MCP ProfileOpen 共用同一派生规则；uuid 含连字符，符合 profile 命名正则。
 */
export function deriveIssueProfile(issueId: string): string {
  return `issue-${issueId}`;
}

/** 任务第 slot 个会话的 profile 名（slot 1 = 基名，2-5 加 -N 后缀；uuid 尾段无「-数字」形态，后缀无歧义）。 */
export function deriveIssueProfileSlot(issueId: string, slot: number): string {
  const base = deriveIssueProfile(issueId);
  return slot > 1 ? `${base}-${slot}` : base;
}

/**
 * 解析任务命名空间 profile 为基座 issueId 与槽位（前后端同规则的解析端）：尾「连字符+
 * 单个数字」且数字在 2-5 识别为槽位后缀，越界数字后缀整体判非任务会话；其余（含 uuid
 * 自带尾缀）为槽位 1 基名。非任务命名空间返回 null。
 */
export function parseTaskSessionProfile(profile: string): { issueId: string; slot: number } | null {
  if (!profile.startsWith('issue-')) {
    return null;
  }
  const id = profile.slice('issue-'.length);
  if (id === '') {
    return null;
  }
  const last = id.charAt(id.length - 1);
  if (id.length >= 2 && id.charAt(id.length - 2) === '-' && last >= '0' && last <= '9') {
    const slot = Number(last);
    if (slot < 2 || slot > TASK_SESSION_SLOT_COUNT) {
      return null;
    }
    return { issueId: id.slice(0, -2), slot };
  }
  return { issueId: id, slot: 1 };
}

/** profile 是否属于任务的会话命名空间（任意槽位）。 */
export function isTaskSessionProfile(issueId: string, profile: string): boolean {
  const parsed = parseTaskSessionProfile(profile);
  return parsed != null && parsed.issueId === issueId;
}

// 会话未释放（引擎进程在场）：starting/ready；idle 已释放、failed 拉起失败/意外退出。
export function isSessionLive(status: BrowserSessionStatus | undefined): boolean {
  return status === 'starting' || status === 'ready';
}
