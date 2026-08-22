// chat 视图数据源（terminal_chat T2.2/T2.3/T3.2）：定位 transcript 路径 → 订阅增量读。
//
// 两段编排（解耦「定位」与「读取」，让 transcript 增量 follow 不再随 claude 状态事件全量重读）：
//   1. 定位（locate effect）：ptyClaudeSession 拿路径 + 存活态 + status；claude 退出后
//      用 localStorage 记忆路径兜底。EVENT_CLAUDE_SESSIONS_CHANGED 触发重定位（status
//      新鲜度）。此段只定位，不读 transcript。
//   2. 订阅（subscribe effect）：路径确定后 transcriptSubscribe 拿初始快照 + 后端记
//      offset 开始 watch；EVENT_TRANSCRIPT_CHANGED（按 path 过滤）增量追加。卸载/切路径
//      时 transcriptUnsubscribe。
//
// 对外状态机由 locate + read 两段派生（deriveState）。

import type {
  ClaudeSessionStatus,
  TranscriptChangedPayload,
  TranscriptMessage,
} from '@src/shared/bindings';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { EVENT_CLAUDE_SESSIONS_CHANGED, EVENT_TRANSCRIPT_CHANGED } from '@src/shared/events';
import { listen } from '@tauri-apps/api/event';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

// 对外状态机：读中 / 无 claude / 空对话 / 错误 / 就绪 / claude 已退出（有历史）。
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
    console.warn('[useTranscript] read transcript path memory failed:', e);
    return null;
  }
}

function saveTranscriptMemory(sessionId: string, transcriptPath: string): void {
  try {
    localStorage.setItem(transcriptMemoryKey(sessionId), transcriptPath);
  } catch (e) {
    console.warn('[useTranscript] save transcript path memory failed:', e);
  }
}

// 按 id 去重合并（offset 重置重读时会重发已见过的行）。
function mergeMessages(
  existing: TranscriptMessage[],
  incoming: TranscriptMessage[],
): TranscriptMessage[] {
  if (incoming.length === 0) {
    return existing;
  }
  const seen = new Set(existing.map(m => m.id));
  const fresh = incoming.filter(m => !seen.has(m.id));
  if (fresh.length === 0) {
    return existing;
  }
  return [...existing, ...fresh];
}

// 定位结果（claude 会话 → transcript 路径 + 存活态）。
type Locate
  = | { status: 'locating' }
    | { status: 'no-claude' }
    | { status: 'error'; message: string }
    | { status: 'located'; path: string; alive: boolean };

// transcript 读取结果（订阅后）。
type Read
  = | { status: 'idle' }
    | { status: 'reading' }
    | { status: 'error'; message: string }
    | { status: 'ready'; messages: TranscriptMessage[] };

// 由两段状态派生对外状态机。
function deriveState(locate: Locate, read: Read): TranscriptState {
  switch (locate.status) {
    case 'locating':
      return { status: 'loading' };
    case 'no-claude':
      return { status: 'no-claude' };
    case 'error':
      return { status: 'error', message: locate.message };
    case 'located':
      if (read.status === 'error') {
        return { status: 'error', message: read.message };
      }
      if (read.status !== 'ready') {
        return { status: 'loading' }; // idle/reading → 读中
      }
      if (locate.alive) {
        return read.messages.length === 0
          ? { status: 'empty' }
          : { status: 'ready', messages: read.messages };
      }
      return { status: 'claude-exited', messages: read.messages };
  }
}

export interface UseTranscriptResult {
  state: TranscriptState;
  // 当前 claude 状态（Busy/Waiting/Idle）；无 claude（未启动/已退出）或未定位时为 null。
  claudeStatus: ClaudeSessionStatus | null;
  // waiting 态上下文（如 "approve Bash"）；非 waiting 为 null。
  waitingFor: string | null;
  refresh: () => void;
}

export function useTranscript(sessionId: string): UseTranscriptResult {
  const [locate, setLocate] = useState<Locate>({ status: 'locating' });
  const [read, setRead] = useState<Read>({ status: 'idle' });
  // claude 状态（composer 发送/停止门槛 + 打字中指示）：alive 时存 sessionRef.status。
  const [claudeStatus, setClaudeStatus] = useState<ClaudeSessionStatus | null>(null);
  // waiting 态上下文（如 "approve Bash"）；非 waiting 为 null。等待 banner 展示用。
  const [waitingFor, setWaitingFor] = useState<string | null>(null);
  // 刷新驱动：自增触发定位 effect 重跑（同 usePtySession attempt 范式）。
  const [reloadKey, setReloadKey] = useState(0);

  // —— 定位 effect：只定位路径 + 存活态 + status，不读 transcript ——
  useEffect(() => {
    let disposed = false;

    const locateOnce = async () => {
      setLocate({ status: 'locating' });
      setClaudeStatus(null);
      setWaitingFor(null);
      let sessionRef;
      try {
        sessionRef = await unwrap(commands.ptyClaudeSession(sessionId));
      } catch (e) {
        if (!disposed) {
          setLocate({
            status: 'error',
            message: e instanceof Error ? e.message : String(e),
          });
        }
        return;
      }
      if (disposed) {
        return;
      }
      if (sessionRef != null) {
        saveTranscriptMemory(sessionId, sessionRef.transcriptPath);
        setClaudeStatus(sessionRef.status);
        setWaitingFor(sessionRef.waitingFor);
        setLocate({
          status: 'located',
          path: sessionRef.transcriptPath,
          alive: true,
        });
      } else {
        const memory = readTranscriptMemory(sessionId);
        if (memory == null) {
          setLocate({ status: 'no-claude' });
        } else {
          setLocate({ status: 'located', path: memory, alive: false });
        }
      }
    };

    void locateOnce();
    const unlisten = listen(EVENT_CLAUDE_SESSIONS_CHANGED, () => {
      void locateOnce();
    });
    return () => {
      disposed = true;
      // 页面重载后 listeners 注册表已清空，旧 eventId 注销报 undefined——无泄漏。
      unlisten
        .then(fn => fn())
        .catch(err =>
          console.debug('[useTranscript] unlisten skipped (page reloaded?):', err),
        );
    };
  }, [sessionId, reloadKey]);

  // —— 订阅 effect：路径确定后 transcriptSubscribe 拿快照 + 增量追加 ——
  const locatedPath = locate.status === 'located' ? locate.path : null;
  // 权威消息累加器：快照落地前作缓冲，落地后作增量追加的唯一真源。用 ref 而非
  // setState updater 内写副作用——React 延迟执行 updater 会让「快照落地前到达的
  // 事件」被孤儿化（丢失）。
  const messagesRef = useRef<TranscriptMessage[]>([]);
  const readyRef = useRef(false);
  // 渲染期重置：locatedPath 变化时重置 read 状态（避免 effect 内同步 setState——
  // react/set-state-in-effect，同 usePtySession attachKey 渲染期调整范式）。
  const lastPathRef = useRef<string | null>(null);
  if (lastPathRef.current !== locatedPath) {
    lastPathRef.current = locatedPath;
    setRead(locatedPath == null ? { status: 'idle' } : { status: 'reading' });
  }

  useEffect(() => {
    if (locatedPath == null) {
      return;
    }
    const path = locatedPath;
    let disposed = false;
    messagesRef.current = [];
    readyRef.current = false;

    // 增量监听先注册，再拿快照：尾部增量在快照落地前到达时先缓冲，落地后合并。
    const unlisten = listen<TranscriptChangedPayload>(EVENT_TRANSCRIPT_CHANGED, (event) => {
      if (disposed) {
        return;
      }
      if (event.payload.path !== path) {
        return;
      }
      const incoming = event.payload.messages;
      // 同步并入累加器（不写进 setState updater——updater 延迟执行会错过缓冲时机）。
      messagesRef.current = mergeMessages(messagesRef.current, incoming);
      if (readyRef.current) {
        setRead({ status: 'ready', messages: messagesRef.current });
      }
    });

    // 首读：拿初始快照 + 后端记 offset 开始 watch。
    void (async () => {
      try {
        const messages = await unwrap(commands.transcriptSubscribe(path));
        if (disposed) {
          return;
        }
        messagesRef.current = mergeMessages(messages, messagesRef.current);
        readyRef.current = true;
        setRead({ status: 'ready', messages: messagesRef.current });
      } catch (e) {
        if (!disposed) {
          setRead({
            status: 'error',
            message: e instanceof Error ? e.message : String(e),
          });
        }
      }
    })();

    return () => {
      disposed = true;
      void commands.transcriptUnsubscribe(path).catch((e: unknown) => {
        console.debug('[useTranscript] unsubscribe skipped:', e);
      });
      unlisten
        .then(fn => fn())
        .catch(err =>
          console.debug('[useTranscript] unlisten skipped (page reloaded?):', err),
        );
    };
  }, [locatedPath]);

  const state = useMemo(() => deriveState(locate, read), [locate, read]);
  const refresh = useCallback(() => setReloadKey(k => k + 1), []);
  return { state, claudeStatus, waitingFor, refresh };
}
