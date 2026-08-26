// claude runtime 状态订阅（claude_orca T2.1）：hook 事件经 Rust 归一化的唯一
// 状态源，事件驱动推送（区别于 useClaudeRunning 的 ps/目录轮询探测）。
//
// 数据流：claude_runtime_state 命令查初值（挂载空窗）+ claude-runtime:changed
// 事件增量（按 payload.pane === sessionId 过滤）。null = 该 pane 无 runtime 条目
// （hook 链路未生效），消费方回落现有轮询链路。
//
// 竞态防御：快照查询链在 listen 注册完成（await）之后——只保证派发顺序不保证
// 注册先完成，注册窗口内到达的事件会丢；事件先到、快照后到时按 lastUpdatedAt
// 拒绝回退覆盖（快照是查询发起时刻的旧值）。

import type { ClaudeRuntimeChangedPayload } from '@src/shared/bindings';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { EVENT_CLAUDE_RUNTIME_CHANGED } from '@src/shared/events';
import { listen } from '@tauri-apps/api/event';
import { useEffect, useRef, useState } from 'react';

/**
 * 订阅一个 pane 的 claude 运行时状态（status/previewText/notification/
 * transcriptPath/claudeSessionId）。
 *
 * @param sessionId pane 锚点（issueId::paneId，与 store key / WE_TERM_PANE 同构）。
 * @returns 最新 runtime 状态；null = 无条目（hook 链路未生效或该 pane 未跑 claude）。
 *   sessionId 变化时重置为 null（组件以 sessionId 为 key，实际罕见）。
 */
export function useClaudeRuntime(sessionId: string): ClaudeRuntimeChangedPayload | null {
  const [runtime, setRuntime] = useState<ClaudeRuntimeChangedPayload | null>(null);
  // 已见事件的最新时间戳：快照晚到时拒绝旧值覆盖（ref 非 state——比较用，不驱动渲染）。
  const latestTsRef = useRef<number>(0);
  // sessionId 变化：渲染期清旧 pane 残留（ref 对比重置范式，同 useTranscript
  // lastPathRef；组件以 sessionId 为 key 实际罕见，hook 是导出 API，防跨会话串台）。
  const lastSessionRef = useRef<string | null>(null);
  if (lastSessionRef.current !== sessionId) {
    lastSessionRef.current = sessionId;
    setRuntime(null);
  }

  useEffect(() => {
    let disposed = false;
    latestTsRef.current = 0;

    const unlisten = listen<ClaudeRuntimeChangedPayload>(EVENT_CLAUDE_RUNTIME_CHANGED, (e) => {
      if (disposed || e.payload.pane !== sessionId) {
        return;
      }
      latestTsRef.current = Math.max(latestTsRef.current, e.payload.lastUpdatedAt);
      setRuntime(e.payload);
    });

    // 快照在 listen 注册完成后查询：注册窗口内到达的事件不丢；若其间事件已到
    // （latestTsRef 前进），旧快照丢弃。
    void (async () => {
      try {
        await unlisten;
        if (disposed) {
          return;
        }
        const snapshot = await unwrap(commands.claudeRuntimeState(sessionId));
        if (disposed) {
          return;
        }
        if (snapshot == null) {
          // 新 pane 无条目：清残留（含 sessionId 切换后旧 listener 清理间隙漏进
          // 的旧 pane 值——渲染期重置之后、effect 重跑之前的微窗口）。
          setRuntime(null);
          return;
        }
        if (snapshot.lastUpdatedAt < latestTsRef.current) {
          return;
        }
        setRuntime(snapshot);
      } catch (e) {
        console.warn('[useClaudeRuntime] load initial state failed:', e);
      }
    })();

    return () => {
      disposed = true;
      // 页面重载后 listeners 注册表已清空，旧 eventId 注销报 undefined——无泄漏
      // （useConfigValue 同款竞态，降 debug 不刷屏）。
      unlisten
        .then(fn => fn())
        .catch(err =>
          console.debug('[useClaudeRuntime] unlisten skipped (page reloaded?):', err),
        );
    };
  }, [sessionId]);

  return runtime;
}
