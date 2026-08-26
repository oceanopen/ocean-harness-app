// chatSendQueue 单测（T3.1）：时间窗串行（二次入队等待不取消）、cancel 清
// timer + onCancelUnsubmitted、空闲同步 start。

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  cancelChatSends,
  enqueueChatSend,
  resetChatSendQueuesForTests,
} from './chatSendQueue';

// 队列序列的录音形态：每步记录调用序号，断言交错关系。
function recordSend(log: string[], tag: string, durationMs: number) {
  return enqueueChatSend(
    'pane::main',
    durationMs,
    ({ delay, markSubmitted }) => {
      log.push(`${tag}:body`);
      delay(durationMs, () => {
        log.push(`${tag}:enter`);
        markSubmitted();
      });
    },
    {
      onCancelUnsubmitted: () => {
        log.push(`${tag}:clear-unsubmitted`);
      },
    },
  );
}

describe('chatSendQueue', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetChatSendQueuesForTests();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('队列空闲时 start 同步执行（正文写入不推迟 tick）', () => {
    const log: string[] = [];
    recordSend(log, 'A', 500);
    expect(log).toEqual(['A:body']);
  });

  it('二次入队等待前序回车窗口，不取消首条（防丢消息）', async () => {
    const log: string[] = [];
    recordSend(log, 'A', 500);
    recordSend(log, 'B', 500);

    // A 回车未发：B 不得开始（否则 B 的清行会杀掉 A 已写入的正文）。
    expect(log).toEqual(['A:body']);
    // async 变体：B 的 execute 挂在 A settled 的微任务链上，需随 timer 一并冲刷。
    await vi.advanceTimersByTimeAsync(500);
    expect(log).toEqual(['A:body', 'A:enter', 'B:body']);
    await vi.advanceTimersByTimeAsync(500);
    expect(log).toEqual(['A:body', 'A:enter', 'B:body', 'B:enter']);
  });

  it('cancel 清延迟回车并回调 onCancelUnsubmitted（清 TUI 残留正文）', () => {
    const log: string[] = [];
    const handle = recordSend(log, 'A', 500);
    expect(handle.bodyStarted()).toBe(true);

    cancelChatSends('pane::main');
    vi.advanceTimersByTime(1000);
    // 回车未发出（timer 已清）+ 残留正文清理回调触发。
    expect(log).toEqual(['A:body', 'A:clear-unsubmitted']);
    expect(handle.finished()).toBe(true);
  });

  it('cancel 幂等：二次 cancel 不重复回调', () => {
    const log: string[] = [];
    const handle = recordSend(log, 'A', 500);
    handle.cancel();
    handle.cancel();
    expect(log).toEqual(['A:body', 'A:clear-unsubmitted']);
  });

  it('已提交序列的 cancel 不触发清残回调', async () => {
    const log: string[] = [];
    const handle = recordSend(log, 'A', 500);
    vi.advanceTimersByTime(500);
    expect(log).toEqual(['A:body', 'A:enter']);

    handle.cancel(); // 已 markSubmitted：不应清行
    expect(log).toEqual(['A:body', 'A:enter']);
    await handle.settled;
    expect(handle.finished()).toBe(true);
  });

  it('多 session 相互独立', () => {
    const logA: string[] = [];
    const logB: string[] = [];
    enqueueChatSend('pane::main', 500, ({ delay, markSubmitted }) => {
      logA.push('A:body');
      delay(500, () => {
        markSubmitted();
      });
    });
    enqueueChatSend('other::main', 500, ({ markSubmitted }) => {
      logB.push('B:body');
      markSubmitted();
    });
    // B 不受 A 占线影响（不同 session 各自串行）。
    expect(logA).toEqual(['A:body']);
    expect(logB).toEqual(['B:body']);
  });
});
