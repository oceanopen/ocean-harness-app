import type { ClaudeSessionInfo, NavErr } from '@src/shared/bindings';
import { Autorenew as AutorenewIcon } from '@mui/icons-material';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  CircularProgress,
  IconButton,
  Snackbar,
  Typography,
} from '@mui/material';
import { commands } from '@src/shared/bindings';
import { countAttentionClaudeSessions } from '@src/shared/claudeSessionStatus';
import { unwrap } from '@src/shared/commands';
import {
  EVENT_CLAUDE_SESSION_NAV_FAILED,
  EVENT_CLAUDE_SESSIONS_CHANGED,
} from '@src/shared/events';
import { listen } from '@tauri-apps/api/event';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ClaudeSessionList from './components/ClaudeSessionList';

type LoadStatus = 'loading' | 'ready' | 'error';

// 浮层 toast 严重级别（成功用 success，错误用 error）。
type ToastSeverity = 'success' | 'warning' | 'error';

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

// panel 窗口「Claude 会话监听」菜单对应的页面：会话加载/事件订阅/汇总栏/刷新/toast。
// 由 PanelApp 右侧内容区渲染，根容器 height:100% 适配父级 flex 容器。
function ClaudeSessionsPage() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<LoadStatus>('loading');
  const [sessions, setSessions] = useState<ClaudeSessionInfo[]>([]);
  const [toast, setToast] = useState<{ text: string; severity: ToastSeverity } | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async () => {
    setStatus('loading');
    try {
      const data = await unwrap(commands.getClaudeSessions());
      setSessions(data);
      setStatus('ready');
    } catch {
      setStatus('error');
    }
  }, []);

  const handleOpenTerminal = useCallback(async (pid: number) => {
    // navigate_to_claude_session 成功路径返回 Ok(())；失败时后端 emit nav-failed，
    // 由下面的 listen 统一处理 toast（不在这里 catch），命令调用本身静默。
    await commands.navigateToClaudeSession(pid);
  }, []);

  // 手动刷新：触发后端 rescan，emit claude-sessions:changed 后订阅自动更新列表与汇总。
  const handleRefresh = useCallback(async () => {
    setRefreshing(true);
    try {
      await unwrap(commands.refreshSessions());
      setToast({ text: t('claudeSessions:toast.refreshSuccess'), severity: 'success' });
    } catch {
      setToast({ text: t('claudeSessions:toast.refreshFail'), severity: 'error' });
    } finally {
      setRefreshing(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    const unlistenPromise = listen<ClaudeSessionInfo[]>(
      EVENT_CLAUDE_SESSIONS_CHANGED,
      (e) => {
        setSessions(e.payload);
      },
    );
    return () => {
      unlistenPromise
        .then(fn => fn())
        .catch((err: unknown) => {
          console.warn('[claude-sessions:changed] unlisten failed (possible Tauri event race):', err);
        });
    };
  }, []);

  useEffect(() => {
    const unlistenPromise = listen<NavErr>(EVENT_CLAUDE_SESSION_NAV_FAILED, (e) => {
      const { key, opts } = navErrToToastKey(e.payload);
      setToast({ text: t(key, opts), severity: 'error' });
    });
    return () => {
      unlistenPromise
        .then(fn => fn())
        .catch((err: unknown) => {
          console.warn('[claude-sessions:nav-failed] unlisten failed:', err);
        });
    };
  }, [t]);

  // 页面展示全部会话（Dead 理论上不出现），ClaudeSessionList 内按优先级排序。
  // summary 计数与 pet_task 对齐，统一用 ATTENTION 口径（Busy+Waiting+GitPending 待关注）。
  const attentionCount = countAttentionClaudeSessions(sessions);

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <Box
        sx={{
          p: 2,
          borderBottom: 1,
          borderColor: 'divider',
          display: 'flex',
          alignItems: 'center',
          gap: 1,
        }}
      >
        <Typography variant="body2" sx={{ fontWeight: 600 }}>
          {t('claudeSessions:summary', { total: sessions.length, attention: attentionCount })}
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
      <Box sx={{ flex: 1, overflow: 'auto' }}>
        {status === 'loading' && (
          <Box
            sx={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
            }}
          >
            <CircularProgress />
          </Box>
        )}
        {status === 'error' && (
          <Box sx={{ p: 2 }}>
            <Alert
              severity="error"
              action={(
                <Button color="inherit" size="small" onClick={load}>
                  {t('claudeSessions:error.retry')}
                </Button>
              )}
            >
              <AlertTitle>{t('claudeSessions:error.title')}</AlertTitle>
              {t('claudeSessions:error.desc')}
            </Alert>
          </Box>
        )}
        {status === 'ready' && (
          <ClaudeSessionList
            sessions={sessions}
            onOpenTerminal={handleOpenTerminal}
          />
        )}
      </Box>
      <Snackbar
        open={toast !== null}
        onClose={() => setToast(null)}
        autoHideDuration={2000}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={toast?.severity ?? 'info'} variant="filled">
          {toast?.text ?? ''}
        </Alert>
      </Snackbar>
    </Box>
  );
}

export default ClaudeSessionsPage;
