import type { ClaudeSessionInfo } from '@src/shared/bindings';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { EVENT_CLAUDE_SESSIONS_CHANGED } from '@src/shared/events';
import { listen } from '@tauri-apps/api/event';
import { useEffect, useState } from 'react';

/**
 * Claude 活跃会话 id 快照（初载 + claude-sessions:changed 事件跟随），树级单订阅。
 * 会话行运行态消费：会话锚 ∈ 快照即运行中；快照不可用按无运行态降级。
 */
export function useClaudeActiveSessionIds(): Set<string> {
  const [ids, setIds] = useState<Set<string>>(() => new Set());
  useEffect(() => {
    let alive = true;
    unwrap(commands.getClaudeSessions())
      .then((list) => {
        if (alive) {
          setIds(new Set(list.map(s => s.sessionId)));
        }
      })
      .catch(() => { /* 快照暂不可用：保持空集，事件到达后自愈 */ });
    const unlisten = listen<ClaudeSessionInfo[]>(EVENT_CLAUDE_SESSIONS_CHANGED, (e) => {
      setIds(new Set(e.payload.map(s => s.sessionId)));
    });
    return () => {
      alive = false;
      unlisten
        .then(fn => fn())
        .catch((err: unknown) => {
          console.warn('[claude-sessions:changed] unlisten failed (possible Tauri event race):', err);
        });
    };
  }, []);
  return ids;
}
