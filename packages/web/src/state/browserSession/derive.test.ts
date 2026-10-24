import { describe, expect, it } from 'vitest';
import { CONVERSATION_SESSION_SLOT_COUNT, deriveConversationProfile, isTaskSessionProfile, parseConversationProfile } from './derive';

/**
 * 会话命名空间 profile 派生/解析单测（D11 双端 SSOT 的前端侧）：与后端 paths_test.go
 * 同一套规则锚点——两位后缀显式存在、越界尾数判非会话实例、归属型别两族。
 */

const taskId = '0197c0a2-7b6a-7333-8b1a-2f4e1d6c9a01';
const convId = '0197c0a2-7b6a-7333-8b1a-2f4e1d6c9a02';

describe('conversation profile 派生与解析', () => {
  it('派生：-01 起两位后缀显式存在（issue / workspace 两族）', () => {
    expect(deriveConversationProfile('issue', taskId, convId, 1)).toBe(`issue-${taskId}_session-${convId}-01`);
    expect(deriveConversationProfile('issue', taskId, convId, 10)).toBe(`issue-${taskId}_session-${convId}-10`);
    expect(deriveConversationProfile('workspace', 'w1', convId, 7)).toBe(`workspace-w1_session-${convId}-07`);
  });

  it('解析 round-trip：归属型别 / ownerID / conversationId / 槽位齐全', () => {
    expect(parseConversationProfile(`issue-${taskId}_session-${convId}-03`)).toEqual({
      ownerKind: 'issue',
      ownerID: taskId,
      conversationId: convId,
      slot: 3,
    });
    expect(parseConversationProfile('workspace-w1_session-c2-07')).toEqual({
      ownerKind: 'workspace',
      ownerID: 'w1',
      conversationId: 'c2',
      slot: 7,
    });
  });

  it('非会话实例判 null：无会话段 / 缺后缀 / 越界槽位 / 缺前缀或空 id', () => {
    expect(parseConversationProfile(`issue-${taskId}`)).toBeNull();
    expect(parseConversationProfile(`issue-${taskId}_session-${convId}`)).toBeNull();
    expect(parseConversationProfile(`issue-${taskId}_session-${convId}-00`)).toBeNull();
    expect(parseConversationProfile(`issue-${taskId}_session-${convId}-11`)).toBeNull();
    expect(parseConversationProfile(`issue-${taskId}_session-${convId}-c9a0`)).toBeNull();
    expect(parseConversationProfile('default')).toBeNull();
    expect(parseConversationProfile(`custom_session-${convId}-01`)).toBeNull();
    expect(parseConversationProfile(`issue-_session-${convId}-01`)).toBeNull();
  });

  it('isTaskSessionProfile 仅按 issue 族 ownerID 匹配', () => {
    expect(isTaskSessionProfile(taskId, deriveConversationProfile('issue', taskId, convId, 1))).toBe(true);
    expect(isTaskSessionProfile(taskId, deriveConversationProfile('workspace', taskId, convId, 1))).toBe(false);
    expect(isTaskSessionProfile('other-task', deriveConversationProfile('issue', taskId, convId, 1))).toBe(false);
  });

  it('槽位上限常量与后端 MaxSessionsPerConversation 对齐', () => {
    expect(CONVERSATION_SESSION_SLOT_COUNT).toBe(10);
  });
});
