// chat 视图数据源（terminal_chat T2.2/T2.3/T3.2 + claude_orca T2.1 切源）：
// 定位 transcript 路径 → 订阅增量读。
//
// 两段编排（解耦「定位」与「读取」，让 transcript 增量 follow 不再随 claude 状态事件全量重读）：
//   1. 定位（locate effect）：claude runtime 的 transcriptPath 优先（hook 载荷直给，
//      T2.1——不受新版 claude transcript 文件名 uuid ≠ session_id 的 cwd 推导不可靠
//      影响；SessionStart 事件即时换路径）；fallback ptyClaudeSession 进程树推导
//      （hook 缺席兜底，兼供 alive 判定）。claude 退出后用 localStorage 记忆路径兜底。
//      EVENT_CLAUDE_SESSIONS_CHANGED 触发重定位。此段只定位，不读 transcript。
//   2. 订阅（subscribe effect）：路径确定后 transcriptSubscribe 拿初始快照 + 后端记
//      offset 开始 watch；EVENT_TRANSCRIPT_CHANGED（按 path 过滤）增量追加。卸载/切路径
//      时 transcriptUnsubscribe。
//
// 对外状态机由 locate + read 两段派生（deriveState）；claudeStatus 由 runtime 优先
// 渲染期合成（见 hook 尾部）。

import type {
  ClaudeNotification,
  ClaudeRuntimeStatus,
  ClaudeSessionStatus,
  TranscriptChangedPayload,
  TranscriptMessage,
} from '@src/shared/bindings';
import type { PendingSend } from './chatPending';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { EVENT_CLAUDE_SESSIONS_CHANGED, EVENT_TRANSCRIPT_CHANGED } from '@src/shared/events';
import { listen } from '@tauri-apps/api/event';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useClaudeRuntime } from '../useClaudeRuntime';
import {
  appendPendingSend,
  pendingSendsAsMessages,
  prunePendingSends,
  readPendingSends,
} from './chatPending';
import { deriveStreamingText, streamingMessage } from './chatStreaming';

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

// runtime 三态 → 现有 PascalCase 会话状态（T2.1 切源）：composer 门槛/waiting
// banner 的判等字面量统一，NativeChatView 派生零改动；GitPending/Dead 是 session
// 轮询的本地派生态，hook 链路下不出现，不映射。
function mapRuntimeStatus(status: ClaudeRuntimeStatus): ClaudeSessionStatus {
  switch (status) {
    case 'idle': return 'Idle';
    case 'working': return 'Busy';
    case 'waiting': return 'Waiting';
  }
}

// transcript 读取结果（订阅后）。
type Read
  = | { status: 'idle' }
    | { status: 'reading' }
    | { status: 'error'; message: string }
    | { status: 'ready'; messages: TranscriptMessage[] };

// 由两段状态派生对外状态机。hasPending：乐观 echo 在场时空会话不判 empty
// （首条消息的 echo 要有 ready 列表可渲染，T3.1）。
function deriveState(locate: Locate, read: Read, hasPending: boolean): TranscriptState {
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
        return read.messages.length === 0 && !hasPending
          ? { status: 'empty' }
          : { status: 'ready', messages: read.messages };
      }
      return { status: 'claude-exited', messages: read.messages };
  }
}

export interface UseTranscriptResult {
  state: TranscriptState;
  // 对外消息（T3.1 合成）：真实 transcript 消息 + 乐观 echo（pending:*）+
  // 流式气泡（streaming，working 且 preview 领先时）。MessageList 直接渲染。
  messages: TranscriptMessage[];
  // 当前 claude 状态（Busy/Waiting/Idle）：locate 确认存活时，runtime 在场走事件
  // 驱动映射（即时），否则回落 session ref（locate 时写入）；无 claude（未启动/已
  // 退出）或未定位时为 null（runtime 条目残留不越过存活判定）。
  claudeStatus: ClaudeSessionStatus | null;
  // waiting 态交互载荷（T4.1 审批卡/提问卡数据源）：PermissionRequest /
  // PreToolUse(AskUserQuestion) 置入，claude 下一个事件清空；存活门槛同
  // claudeStatus（退出后 runtime 残留条目不越权）。
  notification: ClaudeNotification | null;
  // 乐观 echo 登记（T3.1）：发送时先调（真实回写经 onSend prop 走队列）。
  sendEcho: (text: string) => void;
  refresh: () => void;
}

export function useTranscript(sessionId: string): UseTranscriptResult {
  // runtime 状态源（T2.1）：locate 切源 + claudeStatus 切源的优先数据。
  const runtime = useClaudeRuntime(sessionId);
  // runtime 定位判据（任务书 T2.1）：transcriptPath 有值且 claudeSessionId 非空。
  // 派生原始值作 locate effect deps——流式事件（previewText 高频变）不触发重定位，
  // 仅新 SessionStart 绑定新路径时换路径重订阅。
  const runtimePath = runtime?.claudeSessionId != null && runtime.transcriptPath != null
    ? runtime.transcriptPath
    : null;

  const [locate, setLocate] = useState<Locate>({ status: 'locating' });
  const [read, setRead] = useState<Read>({ status: 'idle' });
  // claude 状态（composer 发送/停止门槛 + 打字中指示）：locate 时存 sessionRef.status
  // （fallback 数据源；对外返回值已被 runtime 优先合成覆盖，见 hook 尾部）。
  const [claudeStatus, setClaudeStatus] = useState<ClaudeSessionStatus | null>(null);
  // 刷新驱动：自增触发定位 effect 重跑（同 usePtySession attempt 范式）。
  const [reloadKey, setReloadKey] = useState(0);

  // —— 定位 effect：只定位路径 + 存活态 + status，不读 transcript ——
  useEffect(() => {
    let disposed = false;

    const locateOnce = async () => {
      setLocate({ status: 'locating' });
      setClaudeStatus(null);
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
        // 路径切源（T2.1）：runtime 的 transcriptPath 优先（hook 载荷直给），进程树
        // cwd 推导路径降为 fallback；alive/status 仍以进程树为准（pid 级匹配可靠）。
        const path = runtimePath ?? sessionRef.transcriptPath;
        saveTranscriptMemory(sessionId, path);
        setClaudeStatus(sessionRef.status);
        setLocate({
          status: 'located',
          path,
          alive: true,
        });
      } else {
        // 进程树无 claude：runtime 绑定过的路径仍可定位（claude 已退出，历史可读），
        // 否则回落 localStorage 记忆（claude 存活期间曾打开过 chat 的场景）。
        const memory = runtimePath ?? readTranscriptMemory(sessionId);
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
  }, [sessionId, reloadKey, runtimePath]);

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

  // —— 乐观 echo（T3.1）：模块缓存 keyed by sessionId，chat overlay 开关重挂载
  // 也不丢在途 echo；transcript 每次更新（增量/快照）后 prune。 ——
  const [pending, setPending] = useState<PendingSend[]>(() => readPendingSends(sessionId));
  useEffect(() => {
    const next = prunePendingSends(sessionId, messagesRef.current);
    setPending(prev => (next === prev ? prev : next));
  }, [sessionId, read]);
  const sendEcho = useCallback((text: string) => {
    // 边界 = 发送时最后一条真实消息 id：匹配只认其后消息，防重复文本绑旧 turn。
    const boundary = messagesRef.current.at(-1)?.id ?? null;
    appendPendingSend(sessionId, text, Date.now(), boundary);
    setPending(readPendingSends(sessionId));
  }, [sessionId]);

  const state = useMemo(() => deriveState(locate, read, pending.length > 0), [locate, read, pending]);
  // claudeStatus 切源（T2.1）：runtime 在场即时切（事件驱动，无 locate 轮询滞后），
  // 渲染期合成（无 setState 时序问题）；无 runtime 条目回落 locate 的 session ref
  // 状态（hook 缺席兜底）。映射到 PascalCase，消费方（NativeChatView 门槛派生）零改动。
  // 存活门槛（审查修复）：runtime 条目永不清理（hydrate 每次重启恢复 + 无删除路径），
  // claude 退出后仍非 null——必须以 locate 的进程树存活判定收口：未定位 / 无 claude /
  // 已退出一律 null，恢复禁发语义（claude-exited banner 下 composer 锁定，正文不致
  // 被写进 shell 当命令执行）。
  const alive = locate.status === 'located' && locate.alive;
  const effectiveClaudeStatus = alive
    ? (runtime != null ? mapRuntimeStatus(runtime.status) : claudeStatus)
    : null;
  // notification 切源（T4.1）：runtime 直给（事件驱动即时），存活门槛同上——
  // claude 退出后 runtime 条目残留的 notification 不再渲染交互卡。
  const notification = alive ? (runtime?.notification ?? null) : null;
  // 对外消息合成（T3.1）：真实消息 + 乐观 echo + 流式气泡（working 且 preview
  // 领先于最后 assistant 文本时；真实 turn 落地自然替换——预览被包含即隐藏）。
  // 合成顺序 echo 在流式气泡之前（orca 相反）：「prompt → 回复」顺序语义更
  // 自然，且本仓 Idle 才可发送、两者共存窗口极短。echo 的渲染层 matching
  // 隐藏见 pendingSendsAsMessages（真实 user 行落地即不显示）。
  const messages = useMemo(() => {
    const real = state.status === 'ready' || state.status === 'claude-exited'
      ? state.messages
      : [];
    const composed = [...real, ...pendingSendsAsMessages(pending, real)];
    const streamingText = deriveStreamingText({
      messages: composed,
      previewText: runtime?.previewText,
      working: effectiveClaudeStatus === 'Busy',
    });
    return streamingText != null ? [...composed, streamingMessage(streamingText)] : composed;
  }, [state, pending, runtime, effectiveClaudeStatus]);
  const refresh = useCallback(() => setReloadKey(k => k + 1), []);
  return {
    state,
    messages,
    claudeStatus: effectiveClaudeStatus,
    notification,
    sendEcho,
    refresh,
  };
}
