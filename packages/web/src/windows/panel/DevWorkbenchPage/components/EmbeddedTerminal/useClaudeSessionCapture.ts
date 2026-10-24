import type { PtySessionStatus } from './usePtySession';
import { ConversationService } from '@src/services';
import { commands } from '@src/shared/bindings';
import { EVENT_CLAUDE_SESSIONS_CHANGED } from '@src/shared/events';
import { conversationKeys } from '@src/state/conversation';
import { useQueryClient } from '@tanstack/react-query';
import { listen } from '@tauri-apps/api/event';
import { useEffect, useRef } from 'react';

/**
 * 捕获兜底轮询间隔：事件为主（claude 写 sessions/<pid>.json 后 ~1s 触发事件），5s 仅兜底
 * watcher 未建目录等边缘时序；锚回写时效敏感，比 useClaudeRunning 的 60s 紧。
 */
const POLL_MS = 5_000;

/**
 * useClaudeSessionCapture：终端捕获（§2.3 第 3 步）——pane 子树内 claude 进程的 sessionId 一次性回写 conversation 锚；conversationId 为 null/空（T5.3 接入前常态）整体 inert。
 * 驱动三路照 useClaudeRunning：active 初探 + EVENT_CLAUDE_SESSIONS_CHANGED 重探 + 轮询兜底；「回写成功」或「发现已有非空锚」即永久停。
 * 重开后（active 翻转）重新探测，幂等由 Go 侧条件更新兜底（首捕获胜出）。
 */
export function useClaudeSessionCapture(sessionId: string, status: PtySessionStatus, conversationId: string | null): void {
  const active = status === 'active';
  const qc = useQueryClient();
  const capturedRef = useRef(false);
  const lastActiveRef = useRef(false);
  if (!active && lastActiveRef.current) {
    capturedRef.current = false;
  }
  lastActiveRef.current = active;
  // pane 锚含会话段（每会话独立 pane 集合）：锚更换 = 切容器/切会话（新 pane 全新进程）
  // → 重置重捕；同会话内锚稳定，不误重置。
  const lastSessionRef = useRef<string | null>(null);
  if (sessionId !== lastSessionRef.current) {
    lastSessionRef.current = sessionId;
    capturedRef.current = false;
  }

  useEffect(() => {
    if (!active || conversationId == null || conversationId === '') {
      return;
    }
    let disposed = false;
    const probe = () => {
      if (capturedRef.current) {
        return;
      }
      void commands.ptyClaudeSessionId(sessionId).then(async (sid) => {
        if (disposed || capturedRef.current || sid == null) {
          return;
        }
        const row = await ConversationService.setClaudeSessionId({ id: conversationId, claudeSessionId: sid });
        capturedRef.current = true;
        void qc.invalidateQueries({ queryKey: conversationKeys.root });
        if (row.claudeSessionId !== sid) {
          // 首捕获胜出被他人先占：锚有效即可，留 info 供排障。
          console.info('[useClaudeSessionCapture] anchor owned by other session:', sessionId, row.claudeSessionId);
        }
      }).catch((e: unknown) => {
        console.warn('[useClaudeSessionCapture] probe failed:', sessionId, e);
      });
    };
    probe();
    const timer = window.setInterval(probe, POLL_MS);
    const unlistenSessions = listen(EVENT_CLAUDE_SESSIONS_CHANGED, () => {
      probe();
    });
    return () => {
      disposed = true;
      window.clearInterval(timer);
      // 页面重载后旧 eventId 注销报 undefined——无泄漏的重载竞态，降 debug（useClaudeRunning 同处理）。
      unlistenSessions.then(fn => fn()).catch((err: unknown) => console.debug('[useClaudeSessionCapture] unlisten skipped (page reloaded?):', err));
    };
  }, [sessionId, active, conversationId, qc]);
}
