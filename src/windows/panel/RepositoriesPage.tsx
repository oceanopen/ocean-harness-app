import type { Repository } from '@src/shared/bindings';
import { AddOutlined as AddOutlinedIcon, Autorenew as AutorenewIcon, FolderOutlined as FolderOutlinedIcon } from '@mui/icons-material';
import { Alert, AlertTitle, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, Snackbar, TextField, Typography } from '@mui/material';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import AddRepositoryDialog from './components/AddRepositoryDialog';
import RepositoryCard from './components/RepositoryCard';

type LoadStatus = 'loading' | 'ready' | 'error';

// 浮层 toast 严重级别（成功用 success，失败用 error）。
type ToastSeverity = 'success' | 'error';

// panel 窗口「本地仓库」菜单页面：三态机 + 顶栏(搜索+操作) + 响应式卡片网格 + toast。
// 数据刷新用命令返回值直接 setState（仓库仅本页操作变更，无后台 watcher，故不引入事件）。
// 自动刷新：页面挂载时先 load 展示缓存再 refreshAll 更新 git 信息；
// PanelApp 监听 panel:shown 事件，仅当当前页面为本地仓库管理时通过 windowShownTrigger 触发刷新。
// 默认排序与后端一致（lastCommitAt DESC, id ASC），useMemo 兜底保证新增/刷新后顺序正确。
function RepositoriesPage({ windowShownTrigger }: { windowShownTrigger: number }) {
  const { t } = useTranslation();
  const [status, setStatus] = useState<LoadStatus>('loading');
  const [repos, setRepos] = useState<Repository[]>([]);
  // toast：保留最近一次内容，toastOpen 控制显隐（退出动画期间内容不闪烁）。
  const [toast, setToast] = useState<{ text: string; severity: ToastSeverity }>({ text: '', severity: 'success' });
  const [toastOpen, setToastOpen] = useState(false);
  const [refreshingAll, setRefreshingAll] = useState(false);
  const [refreshingId, setRefreshingId] = useState<number | null>(null);
  const [searchName, setSearchName] = useState('');
  const [searchRemote, setSearchRemote] = useState('');
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<Repository | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Repository | null>(null);
  const [deleting, setDeleting] = useState(false);

  // 防止并发刷新（自动刷新与手动刷新共享同一把锁）
  const refreshingRef = useRef(false);

  const showToast = useCallback((text: string, severity: ToastSeverity) => {
    setToast({ text, severity });
    setToastOpen(true);
  }, []);

  const load = useCallback(async () => {
    setStatus('loading');
    try {
      const data = await unwrap(commands.listRepositories());
      setRepos(data);
      setStatus('ready');
    } catch {
      setStatus('error');
    }
  }, []);

  const refreshAll = useCallback(async () => {
    if (refreshingRef.current) {
      return;
    }
    refreshingRef.current = true;
    setRefreshingAll(true);
    try {
      const list = await unwrap(commands.refreshAllRepositories());
      setRepos(list);
      showToast(t('repositories:toast.refreshAllDone'), 'success');
    } catch (e) {
      showToast(t('repositories:toast.refreshAllFailed', { message: String(e) }), 'error');
    } finally {
      refreshingRef.current = false;
      setRefreshingAll(false);
    }
  }, [t, showToast]);

  // 挂载时：先 load 展示 SQLite 缓存，再 refreshAll 更新 git 信息。
  useEffect(() => {
    (async () => {
      await load();
      void refreshAll();
    })();
  }, []); // 仅挂载时执行

  // PanelApp 通过 windowShownTrigger 通知窗口从隐藏恢复且当前页面为仓库管理页，触发刷新。
  useEffect(() => {
    if (windowShownTrigger > 0 && status === 'ready') {
      void refreshAll();
    }
  }, [windowShownTrigger, status, refreshAll]);

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

  const handleAdded = useCallback((repo: Repository) => {
    setRepos(prev => [...prev, repo]);
    showToast(t('repositories:toast.added', { name: repo.name }), 'success');
  }, [t, showToast]);

  const handleUpdated = useCallback((repo: Repository) => {
    setRepos(prev => prev.map(r => (r.id === repo.id ? repo : r)));
    showToast(t('repositories:toast.updated', { name: repo.name }), 'success');
  }, [t, showToast]);

  const handleRefreshOne = useCallback(async (repo: Repository) => {
    setRefreshingId(repo.id);
    try {
      const updated = await unwrap(commands.refreshRepository(repo.id));
      setRepos(prev => prev.map(r => (r.id === updated.id ? updated : r)));
      showToast(t('repositories:toast.refreshed', { name: repo.name }), 'success');
    } catch (e) {
      showToast(t('repositories:toast.refreshFailed', { message: String(e) }), 'error');
    } finally {
      setRefreshingId(null);
    }
  }, [t, showToast]);

  // 打开回调改为按 dir 传递：卡片「仓库目录」行传仓库根目录，VSCode/iTerm2 经菜单选择后传「仓库目录 + 子目录」。
  const handleOpenFolder = useCallback((dir: string) => {
    unwrap(commands.openInFileManager(dir)).catch((e) => {
      showToast(t('repositories:toast.openFailed', { message: String(e) }), 'error');
    });
  }, [t, showToast]);

  const handleOpenInTerminal = useCallback((dir: string, terminal: 'iterm2' | 'terminal') => {
    unwrap(commands.openInTerminal(terminal, dir)).catch((e) => {
      showToast(t('repositories:toast.openTerminalFailed', { message: String(e) }), 'error');
    });
  }, [t, showToast]);

  const handleConfirmDelete = useCallback(async () => {
    if (!deleteTarget) {
      return;
    }
    setDeleting(true);
    try {
      await unwrap(commands.deleteRepository(deleteTarget.id));
      setRepos(prev => prev.filter(r => r.id !== deleteTarget.id));
      showToast(t('repositories:toast.deleted'), 'success');
      setDeleteTarget(null);
    } catch (e) {
      showToast(t('repositories:toast.deleteFailed', { message: String(e) }), 'error');
    } finally {
      setDeleting(false);
    }
  }, [deleteTarget, t, showToast]);

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
          <IconButton size="small" onClick={() => void refreshAll()} disabled={refreshingAll} aria-label={t('repositories:actions.refresh')}>
            <AutorenewIcon
              sx={{
                'animation': refreshingAll ? 'spin 0.8s linear infinite' : undefined,
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
                <Button color="inherit" size="small" onClick={load}>
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
        autoHideDuration={1000}
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
          onAdded={handleAdded}
        />
      )}

      {editTarget && (
        <AddRepositoryDialog
          repo={editTarget}
          onClose={() => setEditTarget(null)}
          onAdded={handleAdded}
          onUpdated={handleUpdated}
        />
      )}

      <Dialog open={deleteTarget !== null} onClose={deleting ? undefined : () => setDeleteTarget(null)}>
        <DialogTitle>{t('repositories:delete.title')}</DialogTitle>
        <DialogContent>
          <Typography>{t('repositories:delete.confirmMsg', { name: deleteTarget?.name ?? '' })}</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDeleteTarget(null)} disabled={deleting}>
            {t('repositories:delete.cancel')}
          </Button>
          <Button color="error" variant="contained" onClick={handleConfirmDelete} disabled={deleting}>
            {t('repositories:delete.confirm')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

export default RepositoriesPage;
