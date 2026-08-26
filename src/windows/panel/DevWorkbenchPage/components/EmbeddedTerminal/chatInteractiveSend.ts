// chat 交互卡按键步进发送（T4.1，对齐 orca sendNativeChatAskAnswer）：
// 提问卡应答按键组按固定步进间隔逐组写 PTY——组 0 同步写（TUI 即时可见），
// 余组链式推进（写组 i 后才排组 i+1，任一时刻仅一个在途 timer）。
//
// 独立于 chatSendQueue（语义不同，有意不共用）：
//   - 选择器应答无「清行 + 正文 + 延迟回车」序列，组间隔即提交节奏；
//   - 提问卡在场时 composer 已被替换，无第二条消息竞争写入，无需串行队列；
//   - 步进节奏 1s/组远大于队列窗口（500ms），套队列只添等待不添保护。
//
// per-session 键控（模块态，跨组件挂载存续，同 chatSendQueue 取舍）：
//   - 同 session 新应答先取消旧链（用户连续作答，旧链余组不得串场）；
//   - cancelInteractiveSends 供停止/卸载调用，幂等（clear 单个在途 timer 即中止全链）；
//   - 链尾写完删 Map 条目；单组链（组 0 即终态，单问单选最常见形态）不占状态。

import type { AskAnswerKeyGroup } from './NativeChat/chatAsk';
import { buildChatPasteBytes } from './chatSend';

// 步进间隔：500ms 提交间隔 + 500ms 前进缓冲（orca NATIVE_CHAT_QUESTION_STEP_MS
// 同值——慢机/SSH 往返下，下一组写入前选择器还没消化上一组导航键）。
export const CHAT_ASK_STEP_MS = 1000;

interface InteractiveSendState {
  /** 当前在途 timer（链式推进，仅一个）。 */
  timer: ReturnType<typeof setTimeout> | null;
}

const chains = new Map<string, InteractiveSendState>();

/** 中止该 session 在途按键链。幂等：无在途链直接返回。 */
export function cancelInteractiveSends(sessionId: string): void {
  const state = chains.get(sessionId);
  if (!state) {
    return;
  }
  if (state.timer != null) {
    clearTimeout(state.timer);
  }
  chains.delete(sessionId);
}

/** 测试隔离：cancel 在场链并清空全部状态。 */
export function resetInteractiveSendsForTests(): void {
  for (const sessionId of [...chains.keys()]) {
    cancelInteractiveSends(sessionId);
  }
}

/** 单组转字节：raw 原样；text 过 sanitize（含多行 bracketed paste 包裹）。 */
function groupBytes(group: AskAnswerKeyGroup): string {
  return 'raw' in group ? group.raw : buildChatPasteBytes(group.text);
}

/**
 * 按步进间隔逐组写入：组 0 同步，组 i（≥1）在 (i-1) 写入后 CHAT_ASK_STEP_MS
 * 时刻。write 为底层 PTY 写函数（fire-and-forget，与 chatSendQueue 同口径）。
 * 空组链不占状态（卡片 confirm 闸门拦下全未答，防御）。
 */
export function sendInteractiveKeys(
  sessionId: string,
  groups: readonly AskAnswerKeyGroup[],
  write: (bytes: string) => void,
): void {
  // 新应答顶掉旧链：用户改答案重提交，旧链余组不得继续写。
  cancelInteractiveSends(sessionId);
  if (groups.length === 0) {
    return;
  }
  write(groupBytes(groups[0]!));
  if (groups.length === 1) {
    return; // 单组即终态：无余组，不占 Map 状态
  }

  const state: InteractiveSendState = { timer: null };
  chains.set(sessionId, state);
  let index = 1;
  const step = (): void => {
    write(groupBytes(groups[index]!));
    index += 1;
    if (index < groups.length) {
      state.timer = setTimeout(step, CHAT_ASK_STEP_MS);
    } else if (chains.get(sessionId) === state) {
      // 链尾写完即清条目（守卫防误删已顶掉本链的新链，同 chatSendQueue 口径）。
      chains.delete(sessionId);
    }
  };
  state.timer = setTimeout(step, CHAT_ASK_STEP_MS);
}
