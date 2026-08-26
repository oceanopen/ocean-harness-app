// chatInteractiveSend 单测（T4.1）：组 0 同步 / 步进间隔 / cancel 幂等 /
// 新链顶旧链 / text 组过 sanitize。

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  cancelInteractiveSends,
  CHAT_ASK_STEP_MS,
  resetInteractiveSendsForTests,
  sendInteractiveKeys,
} from './chatInteractiveSend';
import { buildChatPasteBytes } from './chatSend';

describe('chatInteractiveSend', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetInteractiveSendsForTests();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('组 0 同步写，后续组按步进间隔（1s/组）逐组写', async () => {
    const writes: string[] = [];
    sendInteractiveKeys('pane::main', [
      { raw: '2' },
      { raw: '\r' },
      { text: '自由文本' },
      { raw: '\x1B[C' },
    ], bytes => writes.push(bytes));

    // 同步：仅组 0 落地。
    expect(writes).toEqual(['2']);
    await vi.advanceTimersByTimeAsync(CHAT_ASK_STEP_MS);
    expect(writes).toEqual(['2', '\r']);
    await vi.advanceTimersByTimeAsync(CHAT_ASK_STEP_MS);
    // text 组过 sanitize（buildChatPasteBytes）。
    expect(writes).toEqual(['2', '\r', buildChatPasteBytes('自由文本')]);
    await vi.advanceTimersByTimeAsync(CHAT_ASK_STEP_MS);
    expect(writes).toEqual(['2', '\r', buildChatPasteBytes('自由文本'), '\x1B[C']);
  });

  it('cancel 中止余组；幂等（重复调用无副作用）', async () => {
    const writes: string[] = [];
    sendInteractiveKeys('pane::main', [{ raw: '1' }, { raw: '2' }, { raw: '3' }], bytes => writes.push(bytes));
    cancelInteractiveSends('pane::main');
    cancelInteractiveSends('pane::main'); // 幂等
    await vi.advanceTimersByTimeAsync(CHAT_ASK_STEP_MS * 3);
    expect(writes).toEqual(['1']);
  });

  it('新链顶旧链：旧链余组不再写，新链组 0 立即落地', async () => {
    const writes: string[] = [];
    sendInteractiveKeys('pane::main', [{ raw: '1' }, { raw: '2' }, { raw: '3' }], bytes => writes.push(bytes));
    await vi.advanceTimersByTimeAsync(CHAT_ASK_STEP_MS);
    expect(writes).toEqual(['1', '2']);

    sendInteractiveKeys('pane::main', [{ raw: '9' }, { raw: '8' }], bytes => writes.push(bytes));
    await vi.advanceTimersByTimeAsync(CHAT_ASK_STEP_MS * 3);
    // 旧链组 2 被顶掉；新链完整走完。
    expect(writes).toEqual(['1', '2', '9', '8']);
  });

  it('per-session 隔离：A 链不受 B 链 cancel 影响', async () => {
    const writesA: string[] = [];
    const writesB: string[] = [];
    sendInteractiveKeys('pane::main', [{ raw: 'a1' }, { raw: 'a2' }], bytes => writesA.push(bytes));
    sendInteractiveKeys('pane::split', [{ raw: 'b1' }, { raw: 'b2' }], bytes => writesB.push(bytes));
    cancelInteractiveSends('pane::split');
    await vi.advanceTimersByTimeAsync(CHAT_ASK_STEP_MS * 2);
    expect(writesA).toEqual(['a1', 'a2']);
    expect(writesB).toEqual(['b1']);
  });

  it('空组链不占状态：cancel 无操作、不误伤后续链', async () => {
    const writes: string[] = [];
    sendInteractiveKeys('pane::main', [], () => writes.push('never'));
    cancelInteractiveSends('pane::main');
    sendInteractiveKeys('pane::main', [{ raw: '1' }], bytes => writes.push(bytes));
    expect(writes).toEqual(['1']);
  });

  it('链尾写完清理状态：后续 cancel 为无操作（不抛错）', async () => {
    const writes: string[] = [];
    sendInteractiveKeys('pane::main', [{ raw: '1' }, { raw: '2' }], bytes => writes.push(bytes));
    await vi.advanceTimersByTimeAsync(CHAT_ASK_STEP_MS * 2);
    expect(writes).toEqual(['1', '2']);
    expect(() => cancelInteractiveSends('pane::main')).not.toThrow();
  });

  it('单组链不占状态（组 0 即终态，单问单选最常见形态）：cancel 无操作不误伤后续链', () => {
    const writes: string[] = [];
    sendInteractiveKeys('pane::main', [{ raw: '2' }], bytes => writes.push(bytes));
    expect(writes).toEqual(['2']);
    expect(() => cancelInteractiveSends('pane::main')).not.toThrow();
    sendInteractiveKeys('pane::main', [{ raw: '1' }], bytes => writes.push(bytes));
    expect(writes).toEqual(['2', '1']);
  });
});
