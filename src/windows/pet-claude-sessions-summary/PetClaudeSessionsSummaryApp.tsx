import type { ClaudeSessionInfo, ClaudeSessionStatus } from '@src/shared/bindings';
import {
  DEFAULT_PET_DRAGGABLE,
  isYes,
  parseYesNo,
  PET_CLAUDE_SESSIONS_SUMMARY_DRAGGABLE_KEY,
} from '@src/shared/app_config';
import { commands } from '@src/shared/bindings';
import { CLAUDE_SESSION_STATUS_PRIORITY, countAttentionClaudeSessions } from '@src/shared/claudeSessionStatus';
import { unwrap } from '@src/shared/commands';
import { EVENT_CLAUDE_SESSIONS_CHANGED } from '@src/shared/events';
import { useConfigValue } from '@src/shared/useConfigValue';
import { usePetHover } from '@src/shared/usePetHover';
import { listen } from '@tauri-apps/api/event';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { useCallback, useEffect, useState } from 'react';
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

// 模块级 decode：稳定引用，避免 useConfigValue 每次渲染重复订阅。
function decodeDraggable(v: string | null): boolean {
  return isYes(parseYesNo(v, DEFAULT_PET_DRAGGABLE));
}

function PetClaudeSessionsSummaryApp() {
  const [status, setStatus] = useState<ClaudeSessionStatus>('Dead');
  const [count, setCount] = useState(0);
  const { hovered, handlers } = usePetHover();
  // 桌宠拖拽开关：开启时可拖拽、点击静默；关闭时不可拖拽、点击打开终端监控页。
  const draggable = useConfigValue(PET_CLAUDE_SESSIONS_SUMMARY_DRAGGABLE_KEY, decodeDraggable, false);

  // 纯函数：从 sessions 快照计算 status + count。PetClaudeSessionsSummaryApp 与 PetClaudeSessionsTaskApp 共用
  // claude-sessions:changed payload 作为数据源，applySessions 保证两端对同一事件的响应原子化，
  // 不再走 IPC 二次拉取（消除高频 rescan 下的版本错位）。
  const applySessions = useCallback((sessions: ClaudeSessionInfo[]) => {
    const agg = aggregateStatus(sessions);
    setStatus(agg.status);
    setCount(agg.count);
  }, []);

  // 初次 mount 主动拉一次（与 PetClaudeSessionsTaskApp 一致）；事件回调直接用 payload 调 applySessions。
  // cleanup 用 .then().catch() 防竞态。
  useEffect(() => {
    unwrap(commands.getClaudeSessions())
      .then(applySessions)
      .catch((e) => {
        console.warn('[pet-claude-sessions-summary] load failed', e);
      });
    const unlisten = listen<ClaudeSessionInfo[]>(EVENT_CLAUDE_SESSIONS_CHANGED, (e) => {
      applySessions(e.payload);
    });
    return () => {
      unlisten
        .then(fn => fn())
        .catch(err => console.warn('[pet-claude-sessions-summary] unlisten failed:', err));
    };
  }, [applySessions]);

  // count 变化时驱动 pet_claude_sessions_task 显隐：count > 0 调 show_pet_claude_sessions_task_window（后端按 pet 可见 && count 裁决），
  // count == 0 调 hide_pet_claude_sessions_task_window。pet_claude_sessions_task 显隐主导权在此，后端 rescan 不再自动联动。
  useEffect(() => {
    const cmd = count > 0 ? commands.showPetClaudeSessionsTaskWindow() : commands.hidePetClaudeSessionsTaskWindow();
    unwrap(cmd).catch((e) => {
      console.warn('[pet-claude-sessions-summary] pet_claude_sessions_task visibility failed', e);
    });
  }, [count]);

  // 开启拖拽：鼠标按下进入原生窗口拖拽（startDragging 会吞掉后续 click，故无需特殊处理）。
  // 关闭拖拽：mouseDown 空转，交由 handleClick 打开终端监控页。
  const handleMouseDown = useCallback(async () => {
    // 点击即高亮：未聚焦窗口的 mouseenter 不触发，mousedown 是"鼠标在窗口内"最可靠的信号。
    handlers.onMouseDown();
    if (!draggable) {
      return;
    }
    try {
      await getCurrentWindow().startDragging();
    } catch (e) {
      console.warn('[pet-claude-sessions-summary] startDragging failed:', e);
    }
  }, [draggable, handlers]);
  // 关闭拖拽模式下点击桌宠打开终端监控页；开启模式下 startDragging 已吞掉 click，兜底再判一次。
  const handleClick = useCallback(async () => {
    if (draggable) {
      return;
    }
    try {
      await unwrap(commands.showPanelWindow('claudeSessions'));
    } catch (e) {
      console.warn('[pet-claude-sessions-summary] open panel failed', e);
    }
  }, [draggable]);

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
        // 拖拽态用 grab 提示可拖动，点击态用 pointer 提示可点击打开监控页。
        cursor: draggable ? 'grab' : 'pointer',
        opacity: hovered ? 1 : 0.3,
        transition: 'opacity 0.2s',
      }}
      onMouseEnter={handlers.onMouseEnter}
      onMouseMove={handlers.onMouseMove}
      onMouseLeave={handlers.onMouseLeave}
      onMouseDown={handleMouseDown}
      onClick={handleClick}
    >
      <PetSprite status={status} count={count} />
    </div>
  );
}

export default PetClaudeSessionsSummaryApp;
