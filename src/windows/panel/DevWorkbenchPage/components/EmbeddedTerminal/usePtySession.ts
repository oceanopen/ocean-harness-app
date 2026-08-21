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
  // 重开：对已退出会话重新走 spawn 编排（后端移除旧会话重起）。
  // claude=true：本次 spawn 强制带 startup_command（一次性覆盖 startupCodeCli
  // 配置——「重开并启动 claude」语义，terminal_03 §3.2）。
  reopen: (claude?: boolean) => void;
  // 关闭终端：ptyShutdown 杀 shell + 移出 store（组件随后进 exited 语义，可重开）
  close: () => void;
}

interface UsePtySessionArgs {
  // PTY 会话锚点（store key），由调用方派生：统一 `${issueId}::${paneId}`
  // （main → `issueId::main`，附加 pane → `issueId::<uuid>`）。
  sessionId: string;
  // 工作目录。null = 未就绪（工作空间根目录未设置）：不进编排（不发 spawn），返回哑会话；
  // 由 null 变有值时（设置页配置后 useConfigValue 事件回写触发重渲染）自动重新编排。
  cwd: string | null;
  // 初始尺寸（挂载后 TerminalView fit 实测会再 resize 校正）
  cols: number;
  rows: number;
  // 启动自动运行的编程 CLI（'' = 不注入，普通 shell）。仅 fresh spawn 生效；
  // 已活会话（reattach/复用）不重注入——切换配置即时重编排但活会话保持原状。
  // 「重开并启动 claude」的一次性覆盖在 hook 内部（startupOverrideRef）合成，
  // 对调用方透明——attach 实参的 startupCodeCli 即合成结果。
  startupCodeCli: string;
  // 输出回调（TerminalView 的 write 桥）；reattach/spawn 复用的 scrollback 也经此一次
  // 送达（replay=true 标记历史回放——TerminalView 在回放窗口抑制查询应答，防乱码）。
  // 要求稳定引用（父层 useCallback([])），本 hook 不做 ref 转发层。
  onData: (text: string, replay?: boolean) => void;
}

// 挂载编排（顺序显式）：exists → 存在则 reattach（scrollback 随返回值一次回放）；
// reattach 报 exited（自然退出会话仍留 store，session.rs 注释）或返回 null
// （exists 与 reattach 之间被并发关闭）→ fallthrough 到 spawn。已退出分支由
// Rust 端「已退出移除重起」语义承接（local_provider.rs spawn 幂等分流）——
// 此前 exited 直接短路 return 导致「重开」永远走不到 spawn（实测缺口，
// terminal_03 任务 3 修复；回放 scrollback 后重起，fresh=false 复用分支亦回放 ring
// ——StrictMode 双挂载下早期输出随第一遍已死的 listener 丢失，须靠 scrollback 补齐）。
// 后端 pty_spawn 幂等，React 19 StrictMode 双挂载安全；unmount 不调 ptyShutdown
// ——会话与 ring 常驻（切 issue/切菜单仅断订阅，回切 reattach 重载）。
async function attach(args: UsePtySessionArgs, onEvent: (e: PtyEvent) => void): Promise<'active' | 'exited'> {
  const { sessionId, cwd, cols, rows } = args;
  if (cwd == null) {
    throw new Error('unreachable: attach called with null cwd (guarded by effect)');
  }
  if (await commands.ptyExists(sessionId)) {
    const channel = new Channel<PtyEvent>();
    channel.onmessage = onEvent;
    const reattached = await unwrap(commands.ptyReattach(sessionId, channel));
    if (reattached != null) {
      if (reattached.scrollback) {
        args.onData(reattached.scrollback, true);
      }
      if (!reattached.exited) {
        return 'active';
      }
      // 自然退出：fallthrough 到 ptySpawn（后端移除旧会话重起，「重开」有效路径）。
    }
  }
  const channel = new Channel<PtyEvent>();
  channel.onmessage = onEvent;
  const spawned = await unwrap(
    commands.ptySpawn({ sessionId, cwd, cols, rows, startupCommand: args.startupCodeCli === 'none' ? undefined : args.startupCodeCli }, channel),
  );
  if (!spawned.fresh && spawned.scrollback) {
    args.onData(spawned.scrollback, true);
  }
  return 'active';
}

export function usePtySession({ sessionId, cwd, cols, rows, startupCodeCli, onData }: UsePtySessionArgs): UsePtySessionResult {
  const [status, setStatus] = useState<PtySessionStatus>('connecting');
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  // 重开/重试驱动：attempt 自增触发 effect 重跑编排
  const [attempt, setAttempt] = useState(0);
  // 一次性 startup_command 覆盖（reopen(true) 置位，attach 取用即清）：
  // 「重开并启动 claude」语义。不走 state——startupCodeCli 在 attachKey 与 effect
  // deps 里，覆盖值持久生效会与后续配置变化耦合，违反一次性语义（函数式范式：
  // 外部触发的瞬时意图 → ref 桥，勿渲染消费）。
  const startupOverrideRef = useRef<'claude' | null>(null);
  // attach 期间 fit 已经上报的最新尺寸：attach 完成前 ptyResize 会因会话不存在
  // 被后端拒（日志实证），就绪后按积压值补发一次，保证 PTY winsize 与前端一致。
  const pendingSizeRef = useRef<{ cols: number; rows: number } | null>(null);
  // 编排标识（sessionId/cwd/attempt 串联）：依赖变化 → 新一轮编排应从 'connecting' 起步。
  // 用「上轮 key 不一致则渲染期重置」替代 effect 内同步 setState（react/set-state-in-effect，
  // 同 PanelApp mounted 标志的渲染期调整模式）——仅重置一次，不触发额外提交。
  const attachKey = `${sessionId}\n${cwd ?? ''}\n${startupCodeCli}\n${attempt}`;
  const lastAttachKeyRef = useRef<string | null>(null);
  if (lastAttachKeyRef.current !== attachKey) {
    lastAttachKeyRef.current = attachKey;
    setStatus('connecting');
    setErrorMessage(null);
  }

  useEffect(() => {
    // 就绪守卫：根目录未设置（cwd=null）不进编排——不发 exists/spawn，杜绝启动期对
    // 不存在目录的无效 spawn（后端 WARN「任务目录不存在」刷屏的源头）。
    // 守卫期间 status 停留 'connecting'（哑会话语义）；父层对 cwd==null 有独立引导分支先渲染。
    if (cwd == null) {
      return;
    }
    let cancelled = false;

    // 事件处理函数式：data → 输出；exit → 终态。cancelled 后到达的旧流静默丢弃。
    const onEvent = (e: PtyEvent) => {
      if (cancelled) {
        return;
      }
      if (e.kind === 'data') {
        onData(e.data);
      } else {
        console.info('[pty] session exit:', sessionId);
        setStatus('exited');
      }
    };

    // 一次性覆盖取用即清（本编排生效后续轮次回落配置值）。
    const override = startupOverrideRef.current;
    startupOverrideRef.current = null;
    attach({ sessionId, cwd, cols, rows, startupCodeCli: override ?? startupCodeCli, onData }, onEvent)
      .then((next) => {
        if (cancelled) {
          return;
        }
        setStatus(next);
        const pending = pendingSizeRef.current;
        if (pending != null && (pending.cols !== cols || pending.rows !== rows)) {
          void unwrap(commands.ptyResize(sessionId, pending.cols, pending.rows)).catch((e: unknown) => {
            console.warn('[pty] pending resize failed:', e);
          });
        }
      })
      .catch((e: unknown) => {
        if (!cancelled) {
          const message = e instanceof Error ? e.message : String(e);
          console.warn('[pty] attach failed:', sessionId, message);
          setStatus('error');
          setErrorMessage(message);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId, cwd, cols, rows, startupCodeCli, attempt, onData]);

  // 以下操作函数均为稳定引用（deps 仅 sessionId），直接交给 TerminalView 接线。
  const write = useCallback((data: string) => {
    void unwrap(commands.ptyWrite(sessionId, data)).catch((e: unknown) => {
      console.warn('[pty] write failed:', e);
    });
  }, [sessionId]);

  const resize = useCallback((c: number, r: number) => {
    pendingSizeRef.current = { cols: c, rows: r };
    void unwrap(commands.ptyResize(sessionId, c, r)).catch((e: unknown) => {
      // attach 进行中会话尚未就绪 → not found 属预期（就绪后 attach.then 补发），降级为 debug。
      console.debug('[pty] resize skipped (session not ready):', e);
    });
  }, [sessionId]);

  const reopen = useCallback((claude?: boolean) => {
    if (claude === true) {
      startupOverrideRef.current = 'claude';
    }
    setAttempt(n => n + 1);
  }, []);

  const close = useCallback(() => {
    void unwrap(commands.ptyShutdown(sessionId))
      .then(() => {
        setStatus('exited');
      })
      .catch((e: unknown) => {
        console.warn('[pty] shutdown failed:', e);
      });
  }, [sessionId]);

  return { status, errorMessage, write, resize, reopen, close };
}
