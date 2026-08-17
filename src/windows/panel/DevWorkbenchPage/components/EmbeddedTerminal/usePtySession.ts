import type { PtyEvent } from '@src/shared/bindings';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { Channel } from '@tauri-apps/api/core';
import { useCallback, useEffect, useRef, useState } from 'react';

export type PtySessionStatus
  // 编排中（exists→reattach→spawn 途中）
  = | 'connecting'
  // shell 存活（reattach 复用或 spawn 新起），实时流已接
    | 'active'
  // shell 已退出（exit 事件或 reattach.exited），可重开
    | 'exited'
  // spawn 失败（如目录不存在），可重试
    | 'error';

export interface UsePtySessionResult {
  status: PtySessionStatus;
  // status === 'error' 时的错误信息（后端 Err 文案，如「任务目录不存在：<路径>」）
  errorMessage: string | null;
  // 键盘输入 → ptyWrite（TerminalView onData 接入；稳定引用）
  write: (data: string) => void;
  // 尺寸变化 → ptyResize（TerminalView onResize 接入；稳定引用）
  resize: (cols: number, rows: number) => void;
  // 重开：对已退出会话重新走 spawn 编排（后端移除旧会话重起）
  reopen: () => void;
  // 关闭终端：ptyShutdown 杀 shell + 移出 store（组件随后进 exited 语义，可重开）
  close: () => void;
}

interface UsePtySessionArgs {
  issueId: string;
  cwd: string;
  // 初始尺寸（挂载后 TerminalView fit 实测会再 resize 校正）
  cols: number;
  rows: number;
  // 输出回调（TerminalView 的 write 桥）；reattach/spawn 复用的 scrollback 也经此一次送达。
  // 要求稳定引用（父层 useCallback([])），本 hook 不做 ref 转发层。
  onData: (text: string) => void;
}

// 挂载编排（顺序显式）：exists → 存在则 reattach（scrollback 随返回值一次回放），
// 不存在（或返回 null，exists 与 reattach 之间被并发关闭）则 spawn
// （fresh=false 复用时回放 ring——StrictMode 双挂载下早期输出随第一遍已死的
// listener 丢失，须靠 scrollback 补齐）。
// 后端 pty_spawn 幂等，React 19 StrictMode 双挂载安全；unmount 不调 ptyShutdown
// ——会话与 ring 常驻（切 issue/切菜单仅断订阅，回切 reattach 重载）。
async function attach(args: UsePtySessionArgs, onEvent: (e: PtyEvent) => void): Promise<'active' | 'exited'> {
  const { issueId, cwd, cols, rows } = args;
  if (await commands.ptyExists(issueId)) {
    const channel = new Channel<PtyEvent>();
    channel.onmessage = onEvent;
    const reattached = await unwrap(commands.ptyReattach(issueId, channel));
    if (reattached != null) {
      if (reattached.scrollback) {
        args.onData(reattached.scrollback);
      }
      return reattached.exited ? 'exited' : 'active';
    }
  }
  const channel = new Channel<PtyEvent>();
  channel.onmessage = onEvent;
  const spawned = await unwrap(commands.ptySpawn({ issueId, cwd, cols, rows }, channel));
  if (!spawned.fresh && spawned.scrollback) {
    args.onData(spawned.scrollback);
  }
  return 'active';
}

export function usePtySession({ issueId, cwd, cols, rows, onData }: UsePtySessionArgs): UsePtySessionResult {
  const [status, setStatus] = useState<PtySessionStatus>('connecting');
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  // 重开/重试驱动：attempt 自增触发 effect 重跑编排
  const [attempt, setAttempt] = useState(0);
  // attach 期间 fit 已经上报的最新尺寸：attach 完成前 ptyResize 会因会话不存在
  // 被后端拒（日志实证），就绪后按积压值补发一次，保证 PTY winsize 与前端一致。
  const pendingSizeRef = useRef<{ cols: number; rows: number } | null>(null);

  useEffect(() => {
    let cancelled = false;
    setStatus('connecting');
    setErrorMessage(null);

    // 事件处理函数式：data → 输出；exit → 终态。cancelled 后到达的旧流静默丢弃。
    const onEvent = (e: PtyEvent) => {
      if (cancelled) {
        return;
      }
      if (e.kind === 'data') {
        onData(e.data);
      } else {
        console.info('[pty] session exit:', issueId);
        setStatus('exited');
      }
    };

    attach({ issueId, cwd, cols, rows, onData }, onEvent)
      .then((next) => {
        if (cancelled) {
          return;
        }
        setStatus(next);
        const pending = pendingSizeRef.current;
        if (pending != null && (pending.cols !== cols || pending.rows !== rows)) {
          void unwrap(commands.ptyResize(issueId, pending.cols, pending.rows)).catch((e: unknown) => {
            console.warn('[pty] pending resize failed:', e);
          });
        }
      })
      .catch((e: unknown) => {
        if (!cancelled) {
          const message = e instanceof Error ? e.message : String(e);
          console.warn('[pty] attach failed:', issueId, message);
          setStatus('error');
          setErrorMessage(message);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [issueId, cwd, cols, rows, attempt, onData]);

  // 以下操作函数均为稳定引用（deps 仅 issueId），直接交给 TerminalView 接线。
  const write = useCallback((data: string) => {
    void unwrap(commands.ptyWrite(issueId, data)).catch((e: unknown) => {
      console.warn('[pty] write failed:', e);
    });
  }, [issueId]);

  const resize = useCallback((c: number, r: number) => {
    pendingSizeRef.current = { cols: c, rows: r };
    void unwrap(commands.ptyResize(issueId, c, r)).catch((e: unknown) => {
      // attach 进行中会话尚未就绪 → not found 属预期（就绪后 attach.then 补发），降级为 debug。
      console.debug('[pty] resize skipped (session not ready):', e);
    });
  }, [issueId]);

  const reopen = useCallback(() => {
    setAttempt(n => n + 1);
  }, []);

  const close = useCallback(() => {
    void unwrap(commands.ptyShutdown(issueId))
      .then(() => {
        setStatus('exited');
      })
      .catch((e: unknown) => {
        console.warn('[pty] shutdown failed:', e);
      });
  }, [issueId]);

  return { status, errorMessage, write, resize, reopen, close };
}
