// chat 只读视图数据源（terminal_chat T2.2 + T2.3）：给定 session_id，定位 transcript
// 路径并全量读取消息列表。触发式刷新（无 transcript:changed 事件——T1.3 最小范围
// 只有全量读）：
//   1. 挂载（进入 chat 模式）即读
//   2. 手动 refresh()
//   3. EVENT_CLAUDE_SESSIONS_CHANGED（claude 状态变化 ~1s 去抖）自动重读
// 流式 follow（tail）留 T3.2，届时本 hook 换事件订阅。
//
// T2.3 边界：claude 退出后进程不再可定位（discover 过滤 dead），但 transcript 文件
// 仍在。本 hook 用 localStorage 记忆最近一次 transcript 路径，退出时读记忆路径展示
// 历史对话，状态机增「claude-exited」态（区分「已退出」与「从未启动」）。

import type { ClaudeSessionStatus, TranscriptMessage } from '@src/shared/bindings';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { EVENT_CLAUDE_SESSIONS_CHANGED } from '@src/shared/events';
import { listen } from '@tauri-apps/api/event';
import { useCallback, useEffect, useState } from 'react';

// 状态机：读中 / 无 claude / 空对话 / 错误 / 就绪 / claude 已退出（有历史）。
export type TranscriptState
  = | { status: 'loading' }
    | { status: 'no-claude' }
    | { status: 'empty' }
    | { status: 'error'; message: string }
    | { status: 'ready'; messages: TranscriptMessage[] }
    | { status: 'claude-exited'; messages: TranscriptMessage[] };

// —— transcript 路径记忆（terminal_chat T2.3）——
// claude 退出后进程不可定位，但 transcript 文件仍在；记忆路径让 chat 退出后仍可
// 展示历史。key 含 sessionId（= issueId::main）按 issue 隔离；每 issue 一条，残留
// 量小，不主动清理。
// 已知边界（方案 1 固有）：仅在「claude 存活期间曾打开过 chat」才会写入记忆；若
// 退出后才首次打开 chat，记忆为空 → 退化为 no-claude（磁盘 transcript 仍在，但前
// 端无后端命令可反查 dead session 的 sessionId）。彻底解决需后端扫 dead session。
const TRANSCRIPT_MEMORY_PREFIX = 'terminal_chat_transcript_';

function transcriptMemoryKey(sessionId: string): string {
  return `${TRANSCRIPT_MEMORY_PREFIX}${sessionId}`;
}

function readTranscriptMemory(sessionId: string): string | null {
  try {
    return localStorage.getItem(transcriptMemoryKey(sessionId));
  } catch (e) {
    // 与 saveTranscriptMemory 对称：localStorage 不可用时读失败视为无记忆，
    // 静默降级到 no-claude，不中断 load() 编排。
    console.warn('[useTranscript] read transcript path memory failed:', e);
    return null;
  }
}

function saveTranscriptMemory(sessionId: string, transcriptPath: string): void {
  try {
    localStorage.setItem(transcriptMemoryKey(sessionId), transcriptPath);
  } catch (e) {
    // localStorage 不可用/满时静默降级：退出态退化为 no-claude，不影响主链路。
    console.warn('[useTranscript] save transcript path memory failed:', e);
  }
}

export interface UseTranscriptResult {
  state: TranscriptState;
  // 当前 claude 状态（Busy/Waiting/Idle）；无 claude（未启动/已退出）或未定位时为 null。
  claudeStatus: ClaudeSessionStatus | null;
  refresh: () => void;
}

export function useTranscript(sessionId: string): UseTranscriptResult {
  const [state, setState] = useState<TranscriptState>({ status: 'loading' });
  // claude 状态（composer 发送/停止门槛）：alive 时存 sessionRef.status，无 claude 置 null。
  const [claudeStatus, setClaudeStatus] = useState<ClaudeSessionStatus | null>(null);
  // 刷新驱动：自增触发 effect 重跑编排（同 usePtySession attempt 范式）。
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    let disposed = false;

    // 显式顺序编排：定位路径 → 全量读 → 状态落地。每次 load 先置 loading（后续
    // 事件/手动刷新触发时短暂 loading 属预期反馈）。竞态：disposed 守卫防卸载后
    // setState；未卸载时多次 load 交错完成，晚到者覆盖早到者，最终态为最近一次。
    const load = async () => {
      setState({ status: 'loading' });
      // 重置状态：加载期间（含失败/无 claude）不可发送，composer 门槛为 false。
      setClaudeStatus(null);
      // 1. 定位 claude 会话（Ok(None)=shell 下无活 claude；Err=cwd 异常）。
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
      // 2. 决定 transcript 路径：活 claude 用定位结果并记忆；退出用记忆路径。
      let alive: boolean;
      let transcriptPath: string | null;
      if (sessionRef != null) {
        alive = true;
        transcriptPath = sessionRef.transcriptPath;
        saveTranscriptMemory(sessionId, transcriptPath);
        setClaudeStatus(sessionRef.status);
      } else {
        alive = false;
        transcriptPath = readTranscriptMemory(sessionId);
      }
      if (transcriptPath == null) {
        setState({ status: 'no-claude' });
        return;
      }
      // 3. 全量读 transcript（活读定位路径；退出读记忆路径）。
      let messages: TranscriptMessage[];
      try {
        messages = await unwrap(commands.transcriptRead(transcriptPath));
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
      // 4. 状态落地：活 claude → ready/empty；已退出 → claude-exited（历史可看）。
      if (alive) {
        setState(
          messages.length === 0 ? { status: 'empty' } : { status: 'ready', messages },
        );
      } else {
        setState({ status: 'claude-exited', messages });
      }
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
  return { state, claudeStatus, refresh };
}
