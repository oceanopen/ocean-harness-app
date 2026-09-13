import type { ClaudeSessionInfo, ClaudeSessionStatus } from '@src/shared/bindings';
import { commands } from '@src/shared/bindings';
import { CLAUDE_SESSION_STATUS_PRIORITY, countAttentionClaudeSessions } from '@src/shared/claudeSessionStatus';
import { unwrap } from '@src/shared/commands';
import { EVENT_CLAUDE_SESSIONS_CHANGED } from '@src/shared/events';
import { usePetHover } from '@src/shared/usePetHover';
import { listen } from '@tauri-apps/api/event';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { useCallback, useEffect, useRef, useState } from 'react';
import PetSprite from './components/PetSprite';

// 状态聚合：取所有会话中"最需关注"的那个作为桌宠展示态（优先级 SSOT: claudeSessionStatus.ts，
// Waiting > GitPending > Busy > Idle > Dead）。GitPending 排在 Idle 前：有未提交改动的空闲
// 会话比纯空闲更值得提醒。空列表时为 Dead（无活跃会话的休眠态）。
function aggregateStatus(sessions: ClaudeSessionInfo[]): { status: ClaudeSessionStatus; count: number } {
  if (sessions.length === 0) {
    return { status: 'Dead', count: 0 };
  }
  const top = [...sessions].sort(
    (a, b) => CLAUDE_SESSION_STATUS_PRIORITY[a.status] - CLAUDE_SESSION_STATUS_PRIORITY[b.status],
  )[0];
  // 数量口径：待关注会话（Busy+Waiting+GitPending），驱动徽章数字与 pet_task 显隐。
  // 含 GitPending：用户 commit 后空闲会话仍需提示。top.status 仍看全部会话，
  // 保证全部空闲时显示 Idle 表情、有 GitPending 时显示 Commit。
  return { status: top.status, count: countAttentionClaudeSessions(sessions) };
}

// 点击/拖拽判定阈值（px）：按下后累计位移 ≥ 此值判定为拖拽意图。5px 对齐 macOS
// 原生窗口拖拽手感——轻微手抖不会被误判为拖拽。
const DRAG_THRESHOLD_PX = 5;

// 一次按下的判定状态：起点坐标 + 是否已进入原生拖拽。
interface PetPress {
  pointerId: number;
  startX: number;
  startY: number;
  dragging: boolean;
}

function PetSessionSummaryApp() {
  const [status, setStatus] = useState<ClaudeSessionStatus>('Dead');
  const [count, setCount] = useState(0);
  const { hovered, handlers } = usePetHover();
  // 点击/拖拽判定状态（按下→松开的一次完整生命周期，不驱动渲染，用 ref）。
  const pressRef = useRef<PetPress | null>(null);

  // 纯函数：从 sessions 快照计算 status + count。PetSessionSummaryApp 与 PetSessionTaskApp 共用
  // claude-sessions:changed payload 作为数据源，applySessions 保证两端对同一事件的响应原子化，
  // 不再走 IPC 二次拉取（消除高频 rescan 下的版本错位）。
  const applySessions = useCallback((sessions: ClaudeSessionInfo[]) => {
    const agg = aggregateStatus(sessions);
    setStatus(agg.status);
    setCount(agg.count);
  }, []);

  // 初次 mount 主动拉一次（与 PetSessionTaskApp 一致）；事件回调直接用 payload 调 applySessions。
  // cleanup 用 .then().catch() 防竞态。
  useEffect(() => {
    unwrap(commands.getClaudeSessions())
      .then(applySessions)
      .catch((e) => {
        console.warn('[pet-session-summary] load failed', e);
      });
    const unlisten = listen<ClaudeSessionInfo[]>(EVENT_CLAUDE_SESSIONS_CHANGED, (e) => {
      applySessions(e.payload);
    });
    return () => {
      unlisten
        .then(fn => fn())
        .catch(err => console.warn('[pet-session-summary] unlisten failed:', err));
    };
  }, [applySessions]);

  // count 变化时驱动 pet_session_task 显隐：count > 0 调 show_pet_session_task_window（后端按 pet 可见 && count 裁决），
  // count == 0 调 hide_pet_session_task_window。pet_session_task 显隐主导权在此，后端 rescan 不再自动联动。
  useEffect(() => {
    const cmd = count > 0 ? commands.showPetSessionTaskWindow() : commands.hidePetSessionTaskWindow();
    unwrap(cmd).catch((e) => {
      console.warn('[pet-session-summary] pet_session_task visibility failed', e);
    });
  }, [count]);

  // 点击/拖拽自动区分（替代旧「是否开启拖拽」开关）：
  //   pointerdown 记录起点（不 startDragging）→ 按住移动 ≥ DRAG_THRESHOLD_PX 才进入
  //   原生拖拽 → 未超阈值松开判定为点击，打开终端监控页。
  // 不再消费 DOM click：startDragging 是异步 IPC，快速点击时 mouseup 在其生效前已被
  // WebView 捕获并合成 click，click/mousedown 双入口无法在 DOM 层互斥——旧版靠开关
  // 二选一即为此折中。现在唯一的点击判定入口是 pointerup，语义天然互斥。
  const handlePointerDown = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      // 仅主键参与判定：右键走 contextmenu（原生菜单），中键无语义。
      if (e.button !== 0) {
        return;
      }
      // 点击即高亮：未聚焦窗口的 mouseenter 不触发，按下是"鼠标在窗口内"最可靠的信号。
      handlers.onMouseDown();
      // pointer capture：保证按住后移出 128x128 窗口仍能收到 pointerup 完成点击判定。
      e.currentTarget.setPointerCapture(e.pointerId);
      pressRef.current = {
        pointerId: e.pointerId,
        startX: e.clientX,
        startY: e.clientY,
        dragging: false,
      };
    },
    [handlers],
  );

  const handlePointerMove = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    const press = pressRef.current;
    if (!press || press.dragging || e.pointerId !== press.pointerId) {
      return;
    }
    const dx = e.clientX - press.startX;
    const dy = e.clientY - press.startY;
    if (dx * dx + dy * dy < DRAG_THRESHOLD_PX * DRAG_THRESHOLD_PX) {
      return;
    }
    // 超过阈值才进入原生拖拽：此刻窗口尚未移动，startDragging 从光标当前位置无缝接管。
    // 之后 pointermove/pointerup 不再到达前端（原生循环接管），pressRef 留待下次
    // pointerdown 重置；若松手早于 IPC 生效，原生循环因无按键按下立即退出，均无害。
    press.dragging = true;
    getCurrentWindow().startDragging().catch((err) => {
      console.warn('[pet-session-summary] startDragging failed:', err);
    });
  }, []);

  const handlePointerUp = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    const press = pressRef.current;
    pressRef.current = null;
    if (!press || press.dragging || e.pointerId !== press.pointerId) {
      return;
    }
    // 位移未超阈值 = 点击：打开终端监控页。
    unwrap(commands.showPanelWindow('claudeSessions')).catch((err) => {
      console.warn('[pet-session-summary] open panel failed', err);
    });
  }, []);

  // 右键弹原生菜单（Rust 侧渲染，不受 128x128 窗口边界裁切），当前仅「隐藏桌宠」。
  const handleContextMenu = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    e.preventDefault();
    unwrap(commands.showPetContextMenu(e.clientX, e.clientY)).catch((err) => {
      console.warn('[pet-session-summary] context menu failed', err);
    });
  }, []);

  return (
    <div
      className="pet-surface"
      style={{
        width: '100%',
        height: '100%',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        userSelect: 'none',
        // 光标样式在 index.css：.pet-surface grab / :active grabbing。
        opacity: hovered ? 1 : 0.5,
        transition: 'opacity 0.3s',
      }}
      onMouseEnter={handlers.onMouseEnter}
      onMouseMove={handlers.onMouseMove}
      onMouseLeave={handlers.onMouseLeave}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
      onContextMenu={handleContextMenu}
    >
      <PetSprite status={status} count={count} />
    </div>
  );
}

export default PetSessionSummaryApp;
