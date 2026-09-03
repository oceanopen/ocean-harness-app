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

// 本编排轮 attach 落定的会话进程形态（T3.3 注入编排消费）：
//   direct   = 全新 spawn 且带 directCommand（CLI 是第一进程——向其补发 `claude\r`
//              会被 CLI REPL 当 prompt 文本，注入编排据此跳过补发）
//   plain    = 全新 spawn 的裸 shell
//   reattach = 复用既有会话（进程形态取决于历史 spawn，前端不可知）
// null = 编排未完成（connecting）或未启动。Rust 侧直启失败（CLI 不在场）会回落
// 裸 shell 但 spawnKind 仍报 direct——此时 claude 本就起不来，等待超时兜底，无分流危害。
export type PtySpawnKind = 'direct' | 'plain' | 'reattach';

export interface UsePtySessionResult {
  status: PtySessionStatus;
  // 本编排轮 attach 落定的进程形态（active/exited 时有值；重编排随 attachKey 重置为 null）
  spawnKind: PtySpawnKind | null;
  // status === 'error' 时的错误信息（后端 Err 文案，如「任务目录不存在：<路径>」）
  errorMessage: string | null;
  // 键盘输入 → ptyWrite（TerminalView onData 接入；稳定引用）
  write: (data: string) => void;
  // 尺寸变化 → ptyResize（TerminalView onResize 接入；稳定引用）
  resize: (cols: number, rows: number) => void;
  // 重开：对已退出会话重新走 spawn 编排（后端移除旧会话重起），
  // 按配置 directCommand 直启（配置 none 裸 shell / 配置 claude 起新会话）。
  reopen: () => void;
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
  // 编排前置（配置就绪闸门）：false = 哑会话（不发 exists/spawn），与 cwd==null 同
  // 语义。调用方等度量/编排相关配置（字号/行高/启动 CLI）读取完成后置 true——
  // 首帧即真实字号 fit、directCommand 首次编排即稳定（attachKey 不因异步配置
  // 变化而重复编排、scrollback 二次回放）。
  enabled: boolean;
  // spawn 尺寸兜底占位（容器不可见等边缘场景 fit 无实测值时使用）。正常时序下
  // attach 直接用 TerminalView fit 实测尺寸（pendingSizeRef），不走「占位 spawn →
  // 事后补发纠正」的滞后 resize 路径（每次纠正都是一次打在已绘制提示符上的
  // SIGWINCH 重绘伪影）。
  cols: number;
  rows: number;
  // CLI 直启命令（claude_orca T5.1，唯一自动执行路径）：非 null 时 PTY 直接
  // spawn CLI，无 shell 中转；CLI 退出即 pane 退出。仅 fresh spawn 生效，
  // 活会话（reattach/复用）不重直启。Rust 侧解析失败（CLI 不在场等）回落
  // 普通裸 shell（warn log），前端无感。
  directCommand: string | null;
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
async function attach(
  args: UsePtySessionArgs,
  onEvent: (e: PtyEvent) => void,
  // 本次 spawn 使用的初始尺寸（TerminalView fit 实测优先，入参常量占位兜底）。
  spawnSize: { cols: number; rows: number },
): Promise<{ next: 'active' | 'exited'; spawned: boolean }> {
  const { sessionId, cwd } = args;
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
        return { next: 'active', spawned: false };
      }
      // 自然退出：fallthrough 到 ptySpawn（后端移除旧会话重起，「重开」有效路径）。
    }
  }
  const channel = new Channel<PtyEvent>();
  channel.onmessage = onEvent;
  const spawned = await unwrap(
    commands.ptySpawn({ sessionId, cwd, cols: spawnSize.cols, rows: spawnSize.rows, directCommand: args.directCommand ?? undefined }, channel),
  );
  if (!spawned.fresh && spawned.scrollback) {
    args.onData(spawned.scrollback, true);
  }
  return { next: 'active', spawned: true };
}

export function usePtySession({ sessionId, cwd, enabled, cols, rows, directCommand, onData }: UsePtySessionArgs): UsePtySessionResult {
  const [status, setStatus] = useState<PtySessionStatus>('connecting');
  const [spawnKind, setSpawnKind] = useState<PtySpawnKind | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  // 重开/重试驱动：attempt 自增触发 effect 重跑编排
  const [attempt, setAttempt] = useState(0);
  // attach 期间 fit 已经上报的最新尺寸：attach 完成前 ptyResize 会因会话不存在
  // 被后端拒（日志实证），就绪后按积压值补发一次，保证 PTY winsize 与前端一致。
  const pendingSizeRef = useRef<{ cols: number; rows: number } | null>(null);
  // status 的 ref 镜像：resize 等稳定引用回调内读当前态（deps 挂 state 会使引用
  // 不稳定，TerminalView onResize 接线要求稳定引用）。
  const statusRef = useRef<PtySessionStatus>('connecting');
  statusRef.current = status;
  // 最近一次已请求后端的尺寸（spawn 请求值 / 已发出的 ptyResize 值）：同值不重
  // 发（内核同尺寸 resize 不触发 SIGWINCH，重发徒增 IPC）。null = 本编排轮尚未
  // 请求过（reattach 场景后端现值未知，attach 完成后须校正一次）。
  const lastRequestedRef = useRef<{ cols: number; rows: number } | null>(null);
  // 编排标识（sessionId/cwd/attempt 串联）：依赖变化 → 新一轮编排应从 'connecting' 起步。
  // 用「上轮 key 不一致则渲染期重置」替代 effect 内同步 setState（react/set-state-in-effect，
  // 同 PanelApp mounted 标志的渲染期调整模式）——仅重置一次，不触发额外提交。
  const attachKey = `${sessionId}\n${cwd ?? ''}\n${directCommand ?? ''}\n${attempt}`;
  const lastAttachKeyRef = useRef<string | null>(null);
  if (lastAttachKeyRef.current !== attachKey) {
    lastAttachKeyRef.current = attachKey;
    setStatus('connecting');
    setSpawnKind(null);
    setErrorMessage(null);
  }

  useEffect(() => {
    // 就绪守卫：配置未就绪（enabled=false）或根目录未设置（cwd=null）不进编排
    // ——不发 exists/spawn，杜绝启动期对不存在目录的无效 spawn（后端 WARN
    // 「任务目录不存在」刷屏的源头）。守卫期间 status 停留 'connecting'
    // （哑会话语义）；父层对 cwd==null / 配置未就绪均有独立分支先渲染。
    if (!enabled || cwd == null) {
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

    // spawn 初始尺寸：fit 实测优先——TerminalView mount fit 先于本 effect 执行
    // （子组件 effect 先跑），pendingSizeRef 已持实测值；占位常量仅兜底容器不可
    // 见（宽高 0 跳过 fit）的边缘场景。先测量后生胎，无「spawn 后纠正」步骤。
    const spawnSize = pendingSizeRef.current ?? { cols, rows };
    attach({ sessionId, cwd, enabled, cols, rows, directCommand, onData }, onEvent, spawnSize)
      .then((result) => {
        if (cancelled) {
          return;
        }
        setStatus(result.next);
        setSpawnKind(result.spawned ? (directCommand ? 'direct' : 'plain') : 'reattach');
        if (result.spawned) {
          lastRequestedRef.current = spawnSize;
        }
        // 尺寸校正（固定时序的收尾步骤）：spawn 已用实测值时通常无事可做；
        // reattach 到几何已变化的活会话（如分屏后重挂载）时对齐一次——首帧
        // fit 与后端现值不一致即发，一致则跳过。
        const pending = pendingSizeRef.current;
        const last = lastRequestedRef.current;
        if (pending != null && (last == null || pending.cols !== last.cols || pending.rows !== last.rows)) {
          lastRequestedRef.current = pending;
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
  }, [enabled, sessionId, cwd, cols, rows, directCommand, attempt, onData]);

  // 以下操作函数均为稳定引用（deps 仅 sessionId），直接交给 TerminalView 接线。
  const write = useCallback((data: string) => {
    void unwrap(commands.ptyWrite(sessionId, data)).catch((e: unknown) => {
      console.warn('[pty] write failed:', e);
    });
  }, [sessionId]);

  const resize = useCallback((c: number, r: number) => {
    // 尺寸台账先行：spawn 与 attach 后校正读的都是这里记录的实测值。
    pendingSizeRef.current = { cols: c, rows: r };
    // 未就绪只记录不发送：会话不存在时 IPC 必然被后端拒，attach 完成后按积压值
    // 统一校正（固定时序：测量 → 生胎 → 收尾校正，无中间态请求）。
    if (statusRef.current !== 'active') {
      return;
    }
    // 与最近一次已请求值一致：无需重发（内核同尺寸 resize 不触发信号）。
    const last = lastRequestedRef.current;
    if (last != null && last.cols === c && last.rows === r) {
      return;
    }
    lastRequestedRef.current = { cols: c, rows: r };
    void unwrap(commands.ptyResize(sessionId, c, r)).catch((e: unknown) => {
      console.debug('[pty] resize skipped (session not ready):', e);
    });
  }, [sessionId]);

  const reopen = useCallback(() => {
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

  return { status, spawnKind, errorMessage, write, resize, reopen, close };
}
