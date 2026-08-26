// chat 发送串行队列（T3.1，对齐 orca native-chat-pty-send-queue.ts）：per-session
// 时间窗串行化「清行 + 正文 + 延迟回车」序列，防快速连发与延迟回车交错。
//
// 语义要点（据 orca 源码修正任务书表述）：
//   - 二次入队**不取消**上一次延迟回车，而是等待前序序列的窗口（freeAt）过期——
//     取消写法会静默丢弃首条消息（正文已写入 TUI、回车被吞，随后新清行杀掉正文）。
//   - 队列空闲时 start 同步执行（正文写入不推迟一个 tick，TUI 即时可见）。
//   - cancel 是显式中止（停止/卸载）：清未触发 timer + onCancelUnsubmitted
//     （补写清行字节清掉已写入未提交的正文）+ 退还本序列占用窗口。
//   - 空闲后删 Map 条目（多 pane 长会话不累积常驻 entry）。
//
// durationMs 即序列占线时长（= 延迟回车毫秒数）：freeAt 按此推进，后续入队的
// start 排到窗口之后执行。

export interface ChatSendQueueHandle {
  cancel: () => void;
  settleAfterMs: number;
  settled: Promise<void>;
  bodyStarted: () => boolean;
  finished: () => boolean;
}

export interface EnqueueChatSendOptions {
  /** cancel 中止「已开始但未提交」（回车未发）的序列时回调——清 TUI 残留正文。 */
  onCancelUnsubmitted?: () => void;
}

/** start 收到的执行上下文（延迟回车经 delay 排程，cancel 自动失效）。 */
export interface ChatSendStartContext {
  isCancelled: () => boolean;
  delay: (ms: number, fn: () => void) => void;
  /** 回车（或完成发送的写入）发出时调用，序列收尾放行后续排队。 */
  markSubmitted: () => void;
}

interface QueueState {
  tail: Promise<void>;
  freeAt: number;
  depth: number;
  handles: Set<ChatSendQueueHandle>;
}

const queues = new Map<string, QueueState>();

function getOrCreateState(sessionId: string): QueueState {
  let state = queues.get(sessionId);
  if (!state) {
    state = { tail: Promise.resolve(), freeAt: Date.now(), depth: 0, handles: new Set() };
    queues.set(sessionId, state);
  }
  return state;
}

/** 测试隔离：cancel 在场序列并清空全部队列。 */
export function resetChatSendQueuesForTests(): void {
  for (const state of queues.values()) {
    for (const handle of state.handles) {
      handle.cancel();
    }
  }
  queues.clear();
}

/** 中止该 session 全部在途/排队发送（含延迟回车）。停止 / 卸载用。 */
export function cancelChatSends(sessionId: string): void {
  const state = queues.get(sessionId);
  if (!state) {
    return;
  }
  for (const handle of state.handles) {
    handle.cancel();
  }
}

/**
 * start 仅在该 session 前序序列完成后执行；队列空闲时同步执行。
 * 序列占线 durationMs（期间含延迟回车窗口），后续入队自然排在其后。
 */
export function enqueueChatSend(
  sessionId: string,
  durationMs: number,
  start: (ctx: ChatSendStartContext) => void,
  options?: EnqueueChatSendOptions,
): ChatSendQueueHandle {
  const now = Date.now();
  const state = getOrCreateState(sessionId);
  const waitMs = Math.max(0, state.freeAt - now);
  const settleAfterMs = waitMs + Math.max(0, durationMs);
  state.freeAt = Math.max(now, state.freeAt) + Math.max(0, durationMs);
  state.depth += 1;

  let cancelled = false;
  let bodyStarted = false;
  let finished = false;
  let submitted = false;
  const timers: ReturnType<typeof setTimeout>[] = [];
  let release: (() => void) | null = null;

  const finishEntry = (): void => {
    if (finished) {
      return;
    }
    finished = true;
    const resolve = release;
    release = null;
    resolve?.();
  };

  const delay = (ms: number, fn: () => void): void => {
    const timer = setTimeout(() => {
      if (!cancelled) {
        fn();
      }
    }, ms);
    timers.push(timer);
  };

  const markSubmitted = (): void => {
    submitted = true;
    finishEntry();
  };

  const execute = (): Promise<void> =>
    new Promise<void>((resolve) => {
      release = resolve;
      if (cancelled) {
        release = null;
        finished = true;
        resolve();
        return;
      }
      bodyStarted = true;
      start({ isCancelled: () => cancelled, delay, markSubmitted });
      if (durationMs <= 0) {
        markSubmitted();
      }
    });

  const runPromise
    = state.depth === 1 && waitMs === 0 ? execute() : state.tail.then(() => execute());

  // handle 在下方赋值后才会被这些闭包调用（let 前置仅为满足声明先于使用）。
  let handle: ChatSendQueueHandle;

  const dropHandle = (): void => {
    state.handles.delete(handle);
  };

  const settleQueueEntry = (): void => {
    state.depth = Math.max(0, state.depth - 1);
    finished = true;
    dropHandle();
    // 全部序列收尾后删 per-session 条目，Map 不随 pane 数常驻增长。
    if (state.depth === 0 && state.handles.size === 0 && queues.get(sessionId) === state) {
      queues.delete(sessionId);
    }
  };

  const settled = runPromise.then(settleQueueEntry, settleQueueEntry);
  state.tail = settled;

  handle = {
    cancel: () => {
      if (cancelled) {
        return;
      }
      cancelled = true;
      for (const timer of timers) {
        clearTimeout(timer);
      }
      const shouldClear = bodyStarted && !submitted;
      // 仅退还本序列窗口：freeAt 坍缩到 now 会低估仍在排队的后续序列占线。
      state.freeAt = Math.max(Date.now(), state.freeAt - Math.max(0, durationMs));
      finishEntry();
      dropHandle();
      if (shouldClear) {
        options?.onCancelUnsubmitted?.();
      }
    },
    settleAfterMs,
    settled,
    bodyStarted: () => bodyStarted,
    finished: () => finished,
  };
  state.handles.add(handle);
  return handle;
}
