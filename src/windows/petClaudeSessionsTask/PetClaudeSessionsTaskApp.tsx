import type { ClaudeSessionInfo, NavErr } from '@src/shared/bindings';
import { Autorenew as AutorenewIcon } from '@mui/icons-material';
import { Box, IconButton, List, Paper, Snackbar, Typography } from '@mui/material';
import { commands } from '@src/shared/bindings';
import { isAttentionClaudeSession, sortClaudeSessions } from '@src/shared/claudeSessionStatus';
import { unwrap } from '@src/shared/commands';
import {
  EVENT_CLAUDE_SESSION_NAV_FAILED,
  EVENT_CLAUDE_SESSIONS_CHANGED,
  EVENT_PET_CLAUDE_SESSIONS_TASK_REFIT,
} from '@src/shared/events';
import { usePetHover } from '@src/shared/usePetHover';
import { listen } from '@tauri-apps/api/event';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ClaudeSessionItem from './components/ClaudeSessionItem';
import PetClaudeSessionsTaskEmptyState from './components/PetClaudeSessionsTaskEmptyState';

// 排序优先级 SSOT（Waiting > GitPending > Busy > Idle > Dead）与 sortClaudeSessions
// 均收敛在 claudeSessionStatus.ts，与 ClaudeSessionList / PetClaudeSessionsSummaryApp 共用。

// 与 ClaudeSessionsPage 共用的 NavErr → toast i18n key 映射（保持两端错误文案一致）。
function navErrToToastKey(err: NavErr): { key: string; opts?: Record<string, unknown> } {
  switch (err.kind) {
    case 'unsupportedHostApp':
      return { key: 'claudeSessions:toast.unsupportedHostApp' };
    case 'osaScriptFailed':
      return { key: 'claudeSessions:toast.osaScriptFailed', opts: { stderr: err.stderr } };
    case 'sessionNotFound':
      return { key: 'claudeSessions:toast.sessionNotFound' };
    case 'io':
      return { key: 'claudeSessions:toast.io', opts: { message: err.message } };
  }
}

// 纯渲染组件：会话列表 + nav 失败 toast。窗口显隐由 pet 前端基于 count 驱动（show_pet_claude_sessions_task_window / hide_pet_claude_sessions_task_window）。
function PetClaudeSessionsTaskApp() {
  const { t } = useTranslation();
  const [sessions, setSessions] = useState<ClaudeSessionInfo[]>([]);
  const [toast, setToast] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const { hovered, handlers, reset } = usePetHover();
  const rootRef = useRef<HTMLDivElement>(null);

  // 初次拉取：失败静默（列表为空时自然展示空状态，不阻塞面板渲染）。
  useEffect(() => {
    unwrap(commands.getClaudeSessions())
      .then(setSessions)
      .catch((e) => {
        console.warn('[pet-claude-sessions-task] load failed', e);
      });
  }, []);

  // 订阅会话变更（fs watcher 1s 去抖 + 兜底轮询）：payload 全量快照直接替换，
  // 实现 count 与状态文案的实时刷新。
  useEffect(() => {
    const unlisten = listen<ClaudeSessionInfo[]>(EVENT_CLAUDE_SESSIONS_CHANGED, (e) => {
      setSessions(e.payload);
    });
    return () => {
      unlisten
        .then(fn => fn())
        .catch((err: unknown) => {
          console.warn('[pet-claude-sessions-task:claude-sessions:changed] unlisten failed:', err);
        });
    };
  }, []);

  // 订阅跳转失败 → toast（文案与 ClaudeSessionsPage 一致）。
  useEffect(() => {
    const unlisten = listen<NavErr>(EVENT_CLAUDE_SESSION_NAV_FAILED, (e) => {
      const { key, opts } = navErrToToastKey(e.payload);
      setToast(t(key, opts));
    });
    return () => {
      unlisten
        .then(fn => fn())
        .catch((err: unknown) => {
          console.warn('[pet-claude-sessions-task:nav-failed] unlisten failed:', err);
        });
    };
  }, [t]);

  // 点击列表项：navigateToClaudeSession 失败走 claude-sessions:nav-failed 事件，此处不 catch。
  // 窗口显隐由 pet 前端基于 count 驱动（调 show_pet_claude_sessions_task_window / hide_pet_claude_sessions_task_window），前端点击后不主动 hide。
  const handleOpenTerminal = useCallback(async (pid: number) => {
    await commands.navigateToClaudeSession(pid);
  }, []);

  // 手动刷新：触发后端 rescan，emit claude-sessions:changed 后订阅自动更新列表与汇总。
  const handleRefresh = useCallback(async () => {
    setRefreshing(true);
    try {
      await unwrap(commands.refreshSessions());
    } finally {
      setRefreshing(false);
    }
  }, []);

  // 重新测量 Paper 实际内容高度并回调 fit_pet_claude_sessions_task（set_size + 重新定位）。
  // 可复用：ResizeObserver（内容尺寸变化）、refit 事件（show / 未来 pet 拖动跟随）均调用它。
  const refit = useCallback(() => {
    const root = rootRef.current;
    if (!root) {
      return;
    }
    const height = root.offsetHeight;
    unwrap(commands.fitPetClaudeSessionsTask(height)).catch((e) => {
      console.warn('[pet-claude-sessions-task] fitPetClaudeSessionsTask failed', e);
    });
  }, []);

  // 监听后端 refit 请求（show_pet_claude_sessions_task_window 在 show 后 emit_to）：重新测量并刷新位置。
  // 统一可复用入口——未来 pet 拖动跟随等"尺寸不变却需重定位"的场景也可复用同一事件。
  useEffect(() => {
    const unlisten = listen(EVENT_PET_CLAUDE_SESSIONS_TASK_REFIT, () => {
      // show / 重定位后立刻 reset：清掉 hide 残留的 hovered 并抵消紧随的合成 mouseenter，确保弹出即暗态。
      reset();
      refit();
    });
    return () => {
      unlisten
        .then(fn => fn())
        .catch((err: unknown) => {
          console.warn('[pet-claude-sessions-task:refit] unlisten failed:', err);
        });
    };
  }, [refit, reset]);

  // 动态高度：ResizeObserver 监听 Paper 实际内容高度变化（会话增减 / 空态切换），
  // rAF 合并同帧多次回调后调 refit（set_size + 重新定位，保持与 pet 中心对齐）。
  // observe 后异步触发首回调，等价于 mount 即 fit，覆盖 show 时用的默认高度。
  useEffect(() => {
    const root = rootRef.current;
    if (!root) {
      return;
    }
    let raf = 0;
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(refit);
    });
    observer.observe(root);
    return () => {
      cancelAnimationFrame(raf);
      observer.disconnect();
    };
  }, [refit]);

  // 展示待关注会话（Busy+Waiting+GitPending）：运行中、等输入、或空闲但有未提交改动。
  // 数量与桌宠徽章一致（ATTENTION 口径），驱动本面板显隐。
  const attentionSessions = sortClaudeSessions(sessions.filter(isAttentionClaudeSession));

  return (
    <Paper
      ref={rootRef}
      elevation={3}
      onMouseEnter={handlers.onMouseEnter}
      onMouseMove={handlers.onMouseMove}
      onMouseLeave={handlers.onMouseLeave}
      onMouseDown={handlers.onMouseDown}
      sx={{
        width: 280,
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        borderRadius: 2,
        opacity: hovered ? 1 : 0.3,
        transition: 'opacity 0.2s',
      }}
    >
      <Box
        sx={{
          px: 1.5,
          py: 1,
          borderBottom: 1,
          borderColor: 'divider',
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
          gap: 1,
        }}
      >
        <Typography variant="subtitle2" sx={{ fontWeight: 700 }}>
          {t('petClaudeSessionsTask:task.summary', { total: sessions.length, attention: attentionSessions.length })}
        </Typography>
        <Box sx={{ flex: 1 }} />
        <IconButton size="small" onClick={handleRefresh} disabled={refreshing} aria-label="refresh">
          <AutorenewIcon
            sx={{
              'animation': refreshing ? 'spin 0.8s linear infinite' : undefined,
              '@keyframes spin': {
                from: { transform: 'rotate(0deg)' },
                to: { transform: 'rotate(360deg)' },
              },
            }}
          />
        </IconButton>
      </Box>
      {attentionSessions.length === 0
        ? (
            <PetClaudeSessionsTaskEmptyState />
          )
        : (
            <List sx={{ flex: 1, overflow: 'auto', p: 0.5 }}>
              {attentionSessions.map(s => (
                <ClaudeSessionItem key={s.pid} session={s} onClick={handleOpenTerminal} />
              ))}
            </List>
          )}
      <Snackbar
        open={toast !== null}
        message={toast ?? ''}
        onClose={() => setToast(null)}
        autoHideDuration={4000}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      />
    </Paper>
  );
}

export default PetClaudeSessionsTaskApp;
