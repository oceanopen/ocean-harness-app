import type { ToastSeverity } from '@src/shared/useToast';
import type { PtySessionStatus, PtySpawnKind } from './usePtySession';
import { commands } from '@src/shared/bindings';
import { EVENT_CLAUDE_SESSIONS_CHANGED } from '@src/shared/events';
import { useDevWorkbenchStore } from '@src/state/devWorkbench';
import { listen } from '@tauri-apps/api/event';
import { useEffect } from 'react';

// 润色命令（T2.2 落地于 ocean-harness 插件；issueId 由 cwd basename 推导——终端
// cwd 恒为 `${baseDir}/${issueId}`，命令无需携带参数，OCEAN_HARNESS_PORT 亦已由
// pty_spawn 注入）。
const REFINE_COMMAND = '/ocean-harness:refine-issue';

// claude 就绪等待上限：超时静默清意图标志（hanging 防护；用户定稿「尽力而为不提示」，
// 用户在终端里自见实际状态）。claude 冷启动秒级、trust 确认等交互场景留足余量。
const READY_TIMEOUT_MS = 30_000;

// 意图过期阈值：编排启动（会话 active）前的搁置上限——闸门卡在未初始化 / issue
// 不存在等场景用户已离开时，30s 就绪超时永不起算、标志无限存留，数小时后正常
// 进入开发会误注入。合法顺延最长场景是跳转后手动初始化 + 大仓库 clone（分钟级），
// 10 分钟内闸门放行注入仍送达；超时静默放弃（宁可重按一次「AI 润色」）。与
// READY_TIMEOUT_MS 职责分离：本值管「闸门前放弃」，后者管「编排启动后 claude 起不来」。
const REFINE_INTENT_EXPIRE_MS = 10 * 60_000;

// 本会话 claude 进程探测（进程树真值）。失败按未运行处理（后续事件/超时兜底）。
async function probeClaude(sessionId: string): Promise<boolean> {
  try {
    return await commands.ptyClaudeRunning(sessionId);
  } catch (e) {
    // warn 级：探测持续失败必须可见（useClaudeRunning 同款教训——静默会把环境
    // 问题伪装成功能缺陷，如 command 未注册的 dev 旧二进制）。
    console.warn('[useRefineInjection] probe failed:', sessionId, e);
    return false;
  }
}

/**
 * 注入编排主体（模块级函数，时序显式、依赖全注入——usePtySession attach 同款范式）：
 *
 *   ① probe 本会话 claude（进程树真值——先测量后使用，不复用组件级 useClaudeRunning：
 *      其 probe 回填存在「先见 false」窗口，自动注入无人监督不可接受该竞态）
 *        ├─ 已运行 → toast 提示手动执行（refine 增量重入虽支持，但注入会打断进行中
 *        │           的 agent 会话——agent-dev 跑任务被打断代价高，改由用户手动决定）
 *        └─ 未运行 → 裸 shell（plain/reattach）补发 `claude\r` 启动；直启（direct）
 *                    不补发（CLI 已是第一进程，补发会被其 REPL 当 prompt 文本）
 *   ② 等 EVENT_CLAUDE_SESSIONS_CHANGED（claude 启动写 session json 落盘，秒级）重
 *      probe 转 true → 写入 REFINE_COMMAND\r → 清意图标志（一次注入）
 *   ③ 30s 超时 → 静默清意图标志
 *
 * 残余小窗口（已知并接受）：reattach 到「claude 启动加载中」会话（跳转与 spawn 撞在
 * claude 落盘前）→ probe false 补发 `claude\r`，claude ready 后多消费一条 "claude"
 * prompt，随后的注入仍正常送达。
 *
 * 返回 cancel：disposed 后途中所有异步步骤短路，定时器/事件监听全清。
 */
function runRefineInjection(args: {
  sessionId: string;
  spawnKind: PtySpawnKind;
  write: (data: string) => void;
  showToast: (text: string, severity: ToastSeverity) => void;
  clearRefine: () => void;
}): () => void {
  const { sessionId, spawnKind, write, showToast, clearRefine } = args;
  let disposed = false;
  const cleanups: Array<() => void> = [];
  // 终态收口（幂等）：前置 disposed 检查 + 置位 disposed 封死所有在飞回调——
  // 「一次注入」是本地保证，不依赖 watcher 去抖等跨端时序默契（finish 后、React
  // cleanup 前的窗口内并发事件 probe 不至二次写入）。cancel（外部 dispose）不
  // 走此处（clearRefine 仅编排自然终态消费，取消不误清意图）。
  const finish = () => {
    if (disposed) {
      return;
    }
    disposed = true;
    cleanups.splice(0).forEach(fn => fn());
    clearRefine();
  };

  void (async () => {
    // ① 分流真值探测。
    if (await probeClaude(sessionId)) {
      if (!disposed) {
        showToast(`claude 正在运行，请在终端中手动执行 ${REFINE_COMMAND}`, 'info');
        finish();
      }
      return;
    }
    if (disposed) {
      return;
    }
    // 裸 shell/reattach 补发启动（直启跳过，见函数注释）。
    if (spawnKind !== 'direct') {
      write('claude\r');
    }
    // ② 事件驱动等待就绪：事件是全局 sessions 目录变化（任意 claude 触发），每次重 probe 本会话。
    const unlisten = await listen(EVENT_CLAUDE_SESSIONS_CHANGED, () => {
      void probeClaude(sessionId).then((running) => {
        if (!disposed && running) {
          write(`${REFINE_COMMAND}\r`);
          finish();
        }
      });
    });
    if (disposed) {
      void unlisten();
      return;
    }
    // UnlistenFn 为同步 void（listen promise 已 await 落定），无 rejection 可挂——
    // 与 useClaudeRunning 的 then/catch 链形态不同（彼处 catch 的是 listen 本身）。
    cleanups.push(() => void unlisten());
    // ③ 超时兜底（hanging 清理，非时序手段——就绪判定全程事件驱动）。debug 留痕：
    // 直启回落裸 shell（spawnKind 误报 direct）等静默失效形态的排障线索。
    const timer = window.setTimeout(() => {
      console.debug('[useRefineInjection] ready timeout, drop refine intent:', sessionId);
      finish();
    }, READY_TIMEOUT_MS);
    cleanups.push(() => window.clearTimeout(timer));
  })().catch((e: unknown) => {
    // 编排自身异常（如 listen IPC 失败）：warn 可见 + 清意图（防悬挂）。
    console.warn('[useRefineInjection] orchestration failed:', sessionId, e);
    finish();
  });

  return () => {
    disposed = true;
    cleanups.splice(0).forEach(fn => fn());
  };
}

interface UseRefineInjectionArgs {
  issueId: string;
  // 编排门槛：仅 main pane 启用（附加 pane 是用户手动工作区，不承载润色入口）。
  enabled: boolean;
  sessionStatus: PtySessionStatus;
  // usePtySession 暴露的本轮 attach 进程形态（active 态必有值）。
  spawnKind: PtySpawnKind | null;
  write: (data: string) => void;
  showToast: (text: string, severity: ToastSeverity) => void;
}

/**
 * 润色命令注入编排（T3.3）：消费 devWorkbench store 的 pendingRefineIssueId
 * （TrackerPage 抽屉「AI 润色」按钮写入），意图指向本 issue 且会话 active 时启动编排。
 * 工作空间未初始化/闸门未放行期间终端不挂载、会话不 active——编排自然顺延，
 * 全链时序确定（active → probe → 等事件 → 注入），无一处延时猜测。
 */
export function useRefineInjection({ issueId, enabled, sessionStatus, spawnKind, write, showToast }: UseRefineInjectionArgs) {
  const pendingRefine = useDevWorkbenchStore(s => s.pendingRefine);
  const clearRefine = useDevWorkbenchStore(s => s.clearRefine);
  const pending = enabled && pendingRefine?.issueId === issueId;

  useEffect(() => {
    if (!pending || sessionStatus !== 'active' || spawnKind == null || pendingRefine == null) {
      return;
    }
    // 意图过期（编排启动前搁置超时——闸门卡住/用户离开场景，见
    // REFINE_INTENT_EXPIRE_MS 注释）：静默放弃，不启动编排。
    if (Date.now() - pendingRefine.requestedAt > REFINE_INTENT_EXPIRE_MS) {
      console.debug('[useRefineInjection] intent expired, drop refine intent:', issueId);
      clearRefine();
      return;
    }
    return runRefineInjection({ sessionId: `${issueId}::main`, spawnKind, write, showToast, clearRefine });
    // write（useCallback[sessionId]）/showToast（useCallback[]）/clearRefine（zustand action）均稳定引用。
  }, [pending, pendingRefine, sessionStatus, spawnKind, write, showToast, clearRefine, issueId]);
}
