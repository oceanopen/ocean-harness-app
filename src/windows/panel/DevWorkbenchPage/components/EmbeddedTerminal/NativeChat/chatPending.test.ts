// chatPending 单测（T3.1）：echo 登记 / 双层剪除（渲染层 matching 隐藏 +
// 缓存层 advanced 剪除）/ 边界防绑旧 turn / 重复文本 occurrence / 时间戳
// 回落 / cancel 显式清除。

import type { TranscriptMessage } from '@src/shared/bindings';
import { describe, expect, it } from 'vitest';
import {
  appendPendingSend,
  clearLastPendingSendByText,
  pendingSendsAsMessages,
  prunePendingSends,
  readPendingSends,
} from './chatPending';

function userMsg(id: string, text: string, timestamp = 1): TranscriptMessage {
  return { id, role: 'User', blocks: [{ type: 'text', text }], timestamp };
}

function assistantMsg(id: string, text: string, timestamp = 2): TranscriptMessage {
  return { id, role: 'Assistant', blocks: [{ type: 'text', text }], timestamp };
}

describe('chatPending', () => {
  // append/prune 无 reset API：用独立 sessionId 隔离各用例。

  it('echo 登记并合成为 user 气泡', () => {
    const entry = appendPendingSend('s1', '你好', 100, null);
    expect(entry.occurrence).toBe(1);
    const msgs = pendingSendsAsMessages(readPendingSends('s1'), []);
    expect(msgs).toHaveLength(1);
    expect(msgs[0].id).toBe(`pending:${entry.id}`);
    expect(msgs[0].role).toBe('User');
    expect(msgs[0].blocks).toEqual([{ type: 'text', text: '你好' }]);
    expect(msgs[0].timestamp).toBe(100);
  });

  it('渲染层：user 行落地即隐藏（回复未落地也无双份，trim 归一容忍）', () => {
    appendPendingSend('s2', '修复登录bug', 100, 'm0');
    // 真实 user 行带首尾空白/换行差异：归一化后匹配 → 立即隐藏。
    const landed = [userMsg('m0', '旧消息'), userMsg('m1', '  修复登录bug\r\n')];
    expect(pendingSendsAsMessages(readPendingSends('s2'), landed)).toHaveLength(0);
    // 缓存仍在（剪除等回复落地，防 transcript 抖动重显）。
    expect(readPendingSends('s2')).toHaveLength(1);
  });

  it('缓存层：回复（非 user 消息）落地后才剪除', () => {
    appendPendingSend('s3', '修复登录bug', 100, 'm0');
    const userOnly = [userMsg('m0', '旧消息'), userMsg('m1', '修复登录bug')];
    expect(prunePendingSends('s3', userOnly)).toHaveLength(1);
    const withReply = [...userOnly, assistantMsg('m2', '已修复')];
    expect(prunePendingSends('s3', withReply)).toHaveLength(0);
  });

  it('排队窗口不误清：user 行未落地、前序回复落地时 echo 保留', () => {
    // 场景：连发的 b 被 claude TUI 排队（回车已写、未进 transcript），a 的
    // 回复先落地——advanced 语义下无同文本定局行，缓存与渲染均保留。
    appendPendingSend('s4', 'b 的消息', 100, 'm0');
    const messages = [userMsg('m0', '历史'), userMsg('m1', 'a 的消息'), assistantMsg('m2', 'a 的回复')];
    expect(prunePendingSends('s4', messages)).toHaveLength(1);
    expect(pendingSendsAsMessages(readPendingSends('s4'), messages)).toHaveLength(1);
  });

  it('边界防护：边界之前的同文本旧 turn 不绑定', () => {
    appendPendingSend('s5', '再来一次', 100, 'm5');
    // 同文本消息在边界之前（m0）：不可代表本次发送。
    const messages = [userMsg('m0', '再来一次'), userMsg('m5', '别的历史'), assistantMsg('m6', '回复')];
    expect(prunePendingSends('s5', messages)).toHaveLength(1);
  });

  it('时间戳回落：边界缺失时早于发送时刻的历史行不计入', () => {
    // 边界 id 不在列表（bounded read 裁剪 / 快照未落地即发送）。
    appendPendingSend('s6', '继续', 200, 'mX');
    const messages = [
      userMsg('m0', '继续', 50),
      userMsg('m1', '无关', 50),
      assistantMsg('m2', '旧回复', 60),
    ];
    expect(prunePendingSends('s6', messages)).toHaveLength(1);
    expect(pendingSendsAsMessages(readPendingSends('s6'), messages)).toHaveLength(1);
  });

  it('同文本连发 occurrence 各绑各的 turn', () => {
    appendPendingSend('s7', '继续', 100, 'm0');
    appendPendingSend('s7', '继续', 200, 'm0');
    const oneLanded = [userMsg('m0', 'x'), userMsg('m1', '继续'), assistantMsg('m2', '回复一')];
    // 缓存层：第一条已定局（m1 + m2），第二条等自己的 turn。
    const rest1 = prunePendingSends('s7', oneLanded);
    expect(rest1).toHaveLength(1);
    expect(rest1[0].occurrence).toBe(2);
    // 渲染层：第一条已隐藏（matching），第二条仍显示。
    expect(pendingSendsAsMessages(readPendingSends('s7'), oneLanded)).toHaveLength(1);
    // 第二条 turn 也定局：全部剪除。
    const twoLanded = [...oneLanded, userMsg('m3', '继续'), assistantMsg('m4', '回复二')];
    expect(prunePendingSends('s7', twoLanded)).toHaveLength(0);
  });

  it('cancel 显式清除最近一条同文本 echo', () => {
    appendPendingSend('s8', '被取消的消息', 100, null);
    appendPendingSend('s8', '另一条', 200, null);
    clearLastPendingSendByText('s8', '被取消的消息');
    const rest = readPendingSends('s8');
    expect(rest).toHaveLength(1);
    expect(rest[0].text).toBe('另一条');
  });
});
