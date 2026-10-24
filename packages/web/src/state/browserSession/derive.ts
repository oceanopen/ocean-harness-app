import type { BrowserSessionStatus } from '@src/services';

/** 单会话浏览器实例槽位数（与后端 browser.MaxSessionsPerConversation 对齐：-01..-10 后缀）。 */
export const CONVERSATION_SESSION_SLOT_COUNT = 10;

const ISSUE_PROFILE_PREFIX = 'issue-';
const WORKSPACE_PROFILE_PREFIX = 'workspace-';
const SESSION_SEGMENT = '_session-';

/** 会话命名空间 profile 的解析结果（ownerKind 与后端 ConversationOwner* 常量同域）。 */
export interface ConversationProfile {
  ownerKind: 'issue' | 'workspace';
  ownerID: string;
  conversationId: string;
  slot: number;
}

/**
 * 会话派生 profile 名（会话↔实例关联 SSOT，D11）：`issue-<taskId>_session-<conversationId>-NN`
 * / `workspace-<workspaceId>_session-…`，槽位两位后缀 -01 起显式存在（无裸基名形态）。
 */
export function deriveConversationProfile(ownerKind: 'issue' | 'workspace', ownerID: string, conversationId: string, slot: number): string {
  const prefix = ownerKind === 'issue' ? ISSUE_PROFILE_PREFIX : WORKSPACE_PROFILE_PREFIX;
  return `${prefix}${ownerID}${SESSION_SEGMENT}${conversationId}-${String(slot).padStart(2, '0')}`;
}

/**
 * 解析会话命名空间 profile（前后端同规则的解析端）：`_session-` 分段 + 尾「-两位数字」
 * 识别为槽位后缀（01-10），越界两位尾数（-00/-11…）整体判非会话实例；其余返回 null。
 */
export function parseConversationProfile(profile: string): ConversationProfile | null {
  const seg = profile.lastIndexOf(SESSION_SEGMENT);
  if (seg < 0) {
    return null;
  }
  const head = profile.slice(0, seg);
  const tail = profile.slice(seg + SESSION_SEGMENT.length);
  if (tail.length < 3 || tail.charAt(tail.length - 3) !== '-') {
    return null;
  }
  const digits = tail.slice(-2);
  if (!/^\d{2}$/.test(digits)) {
    return null;
  }
  const slot = Number(digits);
  if (slot < 1 || slot > CONVERSATION_SESSION_SLOT_COUNT) {
    return null;
  }
  const ownerKind = head.startsWith(ISSUE_PROFILE_PREFIX)
    ? 'issue'
    : head.startsWith(WORKSPACE_PROFILE_PREFIX)
      ? 'workspace'
      : null;
  if (ownerKind == null) {
    return null;
  }
  const ownerID = head.slice(ownerKind === 'issue' ? ISSUE_PROFILE_PREFIX.length : WORKSPACE_PROFILE_PREFIX.length);
  const conversationId = tail.slice(0, -3);
  if (ownerID === '' || conversationId === '') {
    return null;
  }
  return { ownerKind, ownerID, conversationId, slot };
}

/** profile 是否属于该任务的会话命名空间（issue 族任意会话/槽位）。 */
export function isTaskSessionProfile(issueId: string, profile: string): boolean {
  const parsed = parseConversationProfile(profile);
  return parsed != null && parsed.ownerKind === 'issue' && parsed.ownerID === issueId;
}

// 会话未释放（引擎进程在场）：starting/ready；idle 已释放、failed 拉起失败/意外退出。
export function isSessionLive(status: BrowserSessionStatus | undefined): boolean {
  return status === 'starting' || status === 'ready';
}
