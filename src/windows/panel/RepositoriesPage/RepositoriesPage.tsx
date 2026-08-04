import type { LocalRepositoryModel } from '@src/services';
import { AddOutlined as AddOutlinedIcon, Autorenew as AutorenewIcon, FolderOutlined as FolderOutlinedIcon } from '@mui/icons-material';
import { Alert, AlertTitle, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, Snackbar, TextField, Typography } from '@mui/material';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import {
  useDeleteLocalRepository,
  useLocalRepositories,
  useRefreshAllLocalRepositories,
  useRefreshLocalRepository,
} from '@src/state/localRepositories';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import AddRepositoryDialog from './components/AddRepositoryDialog';
import RepositoryCard from './components/RepositoryCard';

type LoadStatus = 'loading' | 'ready' | 'error';

// 浮层 toast 严重级别（成功用 success，失败用 error）。
type ToastSeverity = 'success' | 'error';

// 从未知错误对象提取 message（Go 经 http.ts 抛 new Error(msg)，Rust IPC 抛裸字符串，两者兼容）。
function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

// panel 窗口「本地仓库」菜单页面：三态机 + 顶栏(搜索+操作) + 响应式卡片网格 + toast。
// 数据走 Go 服务（useLocalRepositories + mutation hooks，TanStack Query 缓存）。
// 自动刷新：页面挂载时触发一次 refreshAll；PanelApp 监听 panel:shown 事件，仅当当前页面为本地仓库管理时
// 通过 windowShownTrigger 触发刷新。刷新机制本身是 HTTP request-response（与 tracker 域一致），非事件推送。
// OS 原生动作（打开 Finder/终端）仍走 Rust commands，传入 repo.localDir。
function RepositoriesPage({ windowShownTrigger }: { windowShownTrigger: number }) {
  const { t } = useTranslation();
  const { data, isPending, isError } = useLocalRepositories();
  const refreshAllMu = useRefreshAllLocalRepositories();
  const refreshMu = useRefreshLocalRepository();
  const deleteMu = useDeleteLocalRepository();

  const repos = data ?? [];
  const status: LoadStatus = isPending ? 'loading' : isError ? 'error' : 'ready';

  // toast：保留最近一次内容，toastOpen 控制显隐（退出动画期间内容不闪烁）。
  const [toast, setToast] = useState<{ text: string; severity: ToastSeverity }>({ text: '', severity: 'success' });
  const [toastOpen, setToastOpen] = useState(false);
  const [searchName, setSearchName] = useState('');
  const [searchRemote, setSearchRemote] = useState('');
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<LocalRepositoryModel | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<LocalRepositoryModel | null>(null);

  const showToast = useCallback((text: string, severity: ToastSeverity) => {
    setToast({ text, severity });
    setToastOpen(true);
  }, []);

  // 全量刷新（结构同 handleRefreshOne：mutation + isPending 驱动 loading，handler 内 toast）。
  const handleRefreshAll = useCallback(async () => {
    try {
      await refreshAllMu.mutateAsync();
      showToast(t('repositories:toast.refreshAllDone'), 'success');
    } catch (e) {
      showToast(t('repositories:toast.refreshAllFailed', { message: errMsg(e) }), 'error');
    }
  }, [t, showToast, refreshAllMu]);

  // 页面初始化与页面 shown 均触发全量刷新：windowShownTrigger 由 PanelApp 在 panel:shown 时自增，
  // effect 仅依赖它（不依赖 handler/mutation），不会因重渲染反馈而死循环。
  useEffect(() => {
    void handleRefreshAll();
  }, [windowShownTrigger]);

  // 客户端模糊过滤 + 兜底排序（与后端 ORDER BY 一致），保证增删改后无需重新拉取即有序。
  const displayed = useMemo(() => {
    const nameQ = searchName.trim().toLowerCase();
    const remoteQ = searchRemote.trim().toLowerCase();
    const filtered = repos.filter((r) => {
      if (nameQ && !r.name.toLowerCase().includes(nameQ)) {
        return false;
      }
      if (remoteQ && !r.remoteUrl.toLowerCase().includes(remoteQ)) {
        return false;
      }
      return true;
    });
    return [...filtered].sort((a, b) => b.lastCommitAt - a.lastCommitAt || a.id - b.id);
  }, [repos, searchName, searchRemote]);

  const handleRefreshOne = useCallback(async (repo: LocalRepositoryModel) => {
    try {
      await refreshMu.mutateAsync({ id: repo.id });
      showToast(t('repositories:toast.refreshed', { name: repo.name }), 'success');
    } catch (e) {
      showToast(t('repositories:toast.refreshFailed', { message: errMsg(e) }), 'error');
    }
  }, [t, showToast, refreshMu]);

  // 打开回调改为按 dir 传递：卡片「仓库目录」行传仓库根目录，VSCode/iTerm2 经菜单选择后传「仓库目录 + 子目录」。
  const handleOpenFolder = useCallback((dir: string) => {
    unwrap(commands.openInFileManager(dir)).catch((e) => {
      showToast(t('repositories:toast.openFailed', { message: errMsg(e) }), 'error');
    });
  }, [t, showToast]);

  const handleOpenInTerminal = useCallback((dir: string, terminal: 'iterm2' | 'terminal') => {
    unwrap(commands.openInTerminal(terminal, dir)).catch((e) => {
      showToast(t('repositories:toast.openTerminalFailed', { message: errMsg(e) }), 'error');
    });
  }, [t, showToast]);

  const handleConfirmDelete = useCallback(async () => {
    if (!deleteTarget) {
      return;
    }
    try {
      await deleteMu.mutateAsync(deleteTarget.id);
      showToast(t('repositories:toast.deleted'), 'success');
      setDeleteTarget(null);
    } catch (e) {
      showToast(t('repositories:toast.deleteFailed', { message: errMsg(e) }), 'error');
    }
  }, [deleteTarget, t, showToast, deleteMu]);

  const refreshAllPending = refreshAllMu.isPending;
  // 单卡刷新中：mutation 的 variables.id 标识正在刷新的仓库（用于卡片级 loading 判定）。
  const refreshingId = refreshMu.isPending ? refreshMu.variables?.id ?? null : null;

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 顶栏：搜索表单 + 操作栏 */}
      <Box
        sx={{
          p: 2,
          borderBottom: 1,
          borderColor: 'divider',
          display: 'flex',
          alignItems: 'center',
          gap: 1.5,
          flexWrap: 'wrap',
        }}
      >
        <Typography variant="body2" sx={{ fontWeight: 600, flexShrink: 0 }}>
          {t('repositories:summary', { total: repos.length })}
        </Typography>
        <TextField
          size="small"
          placeholder={t('repositories:search.name')}
          value={searchName}
          onChange={e => setSearchName(e.target.value)}
          sx={{ flexGrow: 1, minWidth: 120 }}
        />
        <TextField
          size="small"
          placeholder={t('repositories:search.remote')}
          value={searchRemote}
          onChange={e => setSearchRemote(e.target.value)}
          sx={{ flexGrow: 1, minWidth: 140 }}
        />
        <Box sx={{ display: 'flex', gap: 1, flexShrink: 0 }}>
          <Button
            variant="contained"
            size="small"
            startIcon={<AddOutlinedIcon />}
            onClick={() => setAddDialogOpen(true)}
          >
            {t('repositories:actions.add')}
          </Button>
          <IconButton size="small" onClick={() => void handleRefreshAll()} disabled={refreshAllPending} aria-label={t('repositories:actions.refresh')}>
            <AutorenewIcon
              sx={{
                'animation': refreshAllPending ? 'spin 0.8s linear infinite' : undefined,
                '@keyframes spin': {
                  from: { transform: 'rotate(0deg)' },
                  to: { transform: 'rotate(360deg)' },
                },
              }}
            />
          </IconButton>
        </Box>
      </Box>

      {/* 内容区 */}
      <Box sx={{ flex: 1, overflow: 'auto' }}>
        {status === 'loading' && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <CircularProgress />
          </Box>
        )}
        {status === 'error' && (
          <Box sx={{ p: 2 }}>
            <Alert
              severity="error"
              action={(
                <Button color="inherit" size="small" onClick={() => refreshAllMu.reset()}>
                  {t('repositories:error.retry')}
                </Button>
              )}
            >
              <AlertTitle>{t('repositories:error.title')}</AlertTitle>
              {t('repositories:error.desc')}
            </Alert>
          </Box>
        )}
        {status === 'ready' && repos.length === 0 && (
          <Box
            sx={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
              gap: 1.5,
              px: 3,
              py: 4,
            }}
          >
            <FolderOutlinedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              {t('repositories:empty.title')}
            </Typography>
            <Typography variant="body2" color="text.secondary" align="center">
              {t('repositories:empty.desc')}
            </Typography>
            <Button variant="contained" startIcon={<AddOutlinedIcon />} onClick={() => setAddDialogOpen(true)} sx={{ mt: 1 }}>
              {t('repositories:actions.add')}
            </Button>
          </Box>
        )}
        {status === 'ready' && repos.length > 0 && displayed.length === 0 && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <Typography variant="body2" color="text.secondary">
              {t('repositories:empty.noMatch')}
            </Typography>
          </Box>
        )}
        {status === 'ready' && displayed.length > 0 && (
          <Box
            sx={{
              p: 2,
              display: 'grid',
              gap: 2,
              // 响应式 1-4 列：窄屏 1 列，随宽度递增到 4 列。
              gridTemplateColumns: {
                xs: '1fr',
                sm: 'repeat(1, 1fr)',
                md: 'repeat(2, 1fr)',
                lg: 'repeat(2, 1fr)',
              },
              alignItems: 'start',
            }}
          >
            {displayed.map(repo => (
              <RepositoryCard
                key={repo.id}
                repo={repo}
                refreshing={refreshingId === repo.id}
                onOpenFolder={handleOpenFolder}
                onOpenInTerminal={handleOpenInTerminal}
                onRefresh={handleRefreshOne}
                onEdit={setEditTarget}
                onDelete={setDeleteTarget}
              />
            ))}
          </Box>
        )}
      </Box>

      <Snackbar
        open={toastOpen}
        autoHideDuration={2000}
        onClose={() => setToastOpen(false)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={toast.severity} variant="filled">
          {toast.text}
        </Alert>
      </Snackbar>

      {addDialogOpen && (
        <AddRepositoryDialog
          onClose={() => setAddDialogOpen(false)}
        />
      )}

      {editTarget && (
        <AddRepositoryDialog
          repo={editTarget}
          onClose={() => setEditTarget(null)}
        />
      )}

      <Dialog open={deleteTarget !== null} onClose={deleteMu.isPending ? undefined : () => setDeleteTarget(null)}>
        <DialogTitle>{t('repositories:delete.title')}</DialogTitle>
        <DialogContent>
          <Typography>{t('repositories:delete.confirmMsg', { name: deleteTarget?.name ?? '' })}</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDeleteTarget(null)} disabled={deleteMu.isPending}>
            {t('repositories:delete.cancel')}
          </Button>
          <Button color="error" variant="contained" onClick={handleConfirmDelete} disabled={deleteMu.isPending}>
            {t('repositories:delete.confirm')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

export default RepositoriesPage;
