// chat 只读视图数据源（terminal_chat T2.2）：给定 session_id，定位 transcript 路径
// 并全量读取消息列表。触发式刷新（无 transcript:changed 事件——T1.3 最小范围只有全量读）：
//   1. 挂载（进入 chat 模式）即读
//   2. 手动 refresh()
//   3. EVENT_CLAUDE_SESSIONS_CHANGED（claude 状态变化 ~1s 去抖）自动重读
// 流式 follow（tail）留 T3.2，届时本 hook 换事件订阅。

import type { TranscriptMessage } from '@src/shared/bindings';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { EVENT_CLAUDE_SESSIONS_CHANGED } from '@src/shared/events';
import { listen } from '@tauri-apps/api/event';
import { useCallback, useEffect, useState } from 'react';

// 状态机：读中 / 无 claude / 空对话 / 错误 / 就绪。
export type TranscriptState
  = | { status: 'loading' }
    | { status: 'no-claude' }
    | { status: 'empty' }
    | { status: 'error'; message: string }
    | { status: 'ready'; messages: TranscriptMessage[] };

export interface UseTranscriptResult {
  state: TranscriptState;
  refresh: () => void;
}

export function useTranscript(sessionId: string): UseTranscriptResult {
  const [state, setState] = useState<TranscriptState>({ status: 'loading' });
  // 刷新驱动：自增触发 effect 重跑编排（同 usePtySession attempt 范式）。
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    let disposed = false;

    // 显式顺序编排：定位路径 → 全量读 → 状态落地。每次 load 先置 loading（后续
    // 事件/手动刷新触发时短暂 loading 属预期反馈）。竞态：disposed 守卫防卸载后
    // setState；未卸载时多次 load 交错完成，晚到者覆盖早到者，最终态为最近一次。
    const load = async () => {
      setState({ status: 'loading' });
      // 1. 定位 transcript 路径（Ok(None)=无 claude；Err=cwd 异常）。
      let sessionRef;
      try {
        sessionRef = await unwrap(commands.ptyClaudeSession(sessionId));
      } catch (e) {
        if (!disposed) {
          setState({
            status: 'error',
            message: e instanceof Error ? e.message : String(e),
          });
        }
        return;
      }
      if (disposed) {
        return;
      }
      if (sessionRef == null) {
        setState({ status: 'no-claude' });
        return;
      }
      // 2. 全量读 transcript。
      let messages: TranscriptMessage[];
      try {
        messages = await unwrap(commands.transcriptRead(sessionRef.transcriptPath));
      } catch (e) {
        if (!disposed) {
          setState({
            status: 'error',
            message: e instanceof Error ? e.message : String(e),
          });
        }
        return;
      }
      if (disposed) {
        return;
      }
      setState(
        messages.length === 0 ? { status: 'empty' } : { status: 'ready', messages },
      );
    };

    void load();
    const unlisten = listen(EVENT_CLAUDE_SESSIONS_CHANGED, () => {
      void load();
    });
    return () => {
      disposed = true;
      // 页面重载后 listeners 注册表已清空，旧 eventId 注销报 undefined——无泄漏，
      // 重载竞态已知形态，降 debug（同 useClaudeRunning 处理）。
      unlisten
        .then(fn => fn())
        .catch(err =>
          console.debug('[useTranscript] unlisten skipped (page reloaded?):', err),
        );
    };
  }, [sessionId, reloadKey]);

  const refresh = useCallback(() => setReloadKey(k => k + 1), []);
  return { state, refresh };
}
