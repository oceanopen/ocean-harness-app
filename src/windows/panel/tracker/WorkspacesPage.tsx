import type { WorkspaceModel } from '@src/services';
import {
  AddOutlined as AddOutlinedIcon,
  DeleteOutlined as DeleteOutlinedIcon,
  EditOutlined as EditOutlinedIcon,
  TagOutlined as TagOutlinedIcon,
  WorkspacesOutlined as WorkspacesOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  IconButton,
  Snackbar,
  TextField,
  Typography,
} from '@mui/material';
import { WorkspaceService } from '@src/services';
import { formatDate, formatRelativeTime } from '@src/shared/time';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import WorkspaceDialog from './components/WorkspaceDialog';

type LoadStatus = 'loading' | 'ready' | 'error';

// 浮层 toast 严重级别（成功用 success，失败用 error）。
type ToastSeverity = 'success' | 'error';

const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

// WorkspaceModel 类型与请求封装已迁移至 @src/services（WorkspaceService）。

interface WorkspacesPageProps {
  onSelect: (ws: WorkspaceModel) => void;
}

// 工作空间管理页（tracker 窗口未选中工作空间时的全屏视图）。
// 三态机 + 顶栏(搜索+新建) + 响应式卡片网格 + toast，照搬 RepositoriesPage 范式。
// 数据刷新用接口返回值直接 setState（仅本页操作变更，无后台 watcher，故不引入事件）。
// 选中某卡片经 onSelect 回调上抛，由 TrackerApp 切换到三栏工作视图。
function WorkspacesPage({ onSelect }: WorkspacesPageProps) {
  const { t } = useTranslation();
  const [status, setStatus] = useState<LoadStatus>('loading');
  const [workspaces, setWorkspaces] = useState<WorkspaceModel[]>([]);
  // toast：保留最近一次内容，toastOpen 控制显隐（退出动画期间内容不闪烁）。
  const [toast, setToast] = useState<{ text: string; severity: ToastSeverity }>({ text: '', severity: 'success' });
  const [toastOpen, setToastOpen] = useState(false);
  const [searchName, setSearchName] = useState('');
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<WorkspaceModel | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<WorkspaceModel | null>(null);
  const [deleting, setDeleting] = useState(false);

  const showToast = useCallback((text: string, severity: ToastSeverity) => {
    setToast({ text, severity });
    setToastOpen(true);
  }, []);

  const load = useCallback(async () => {
    setStatus('loading');
    try {
      const data = await WorkspaceService.getList();
      setWorkspaces(data);
      setStatus('ready');
    } catch {
      setStatus('error');
    }
  }, []);

  // 挂载时加载一次。
  useEffect(() => {
    void load();
  }, [load]);

  // 客户端模糊过滤 + 兜底排序（id DESC，新建在前），保证增删改后无需重新拉取即有序。
  const displayed = useMemo(() => {
    const q = searchName.trim().toLowerCase();
    const filtered = workspaces.filter(w => !q || w.name.toLowerCase().includes(q));
    return [...filtered].sort((a, b) => b.id - a.id);
  }, [workspaces, searchName]);

  const handleCreated = useCallback((ws: WorkspaceModel) => {
    setWorkspaces(prev => [...prev, ws]);
    showToast(t('tracker:toast.created', { name: ws.name }), 'success');
  }, [t, showToast]);

  const handleUpdated = useCallback((ws: WorkspaceModel) => {
    setWorkspaces(prev => prev.map(w => (w.id === ws.id ? ws : w)));
    showToast(t('tracker:toast.updated', { name: ws.name }), 'success');
  }, [t, showToast]);

  const handleConfirmDelete = useCallback(async () => {
    if (!deleteTarget) {
      return;
    }
    setDeleting(true);
    try {
      await WorkspaceService.delete({ id: deleteTarget.id });
      setWorkspaces(prev => prev.filter(w => w.id !== deleteTarget.id));
      showToast(t('tracker:toast.deleted'), 'success');
      setDeleteTarget(null);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:toast.deleteFailed', { message: msg }), 'error');
    } finally {
      setDeleting(false);
    }
  }, [deleteTarget, t, showToast]);

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 顶栏：计数 + 搜索 + 新建 */}
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
          {t('tracker:workspace.summary', { total: workspaces.length })}
        </Typography>
        <TextField
          size="small"
          placeholder={t('tracker:workspace.search')}
          value={searchName}
          onChange={e => setSearchName(e.target.value)}
          sx={{ flexGrow: 1, minWidth: 120 }}
        />
        <Button
          variant="contained"
          size="small"
          startIcon={<AddOutlinedIcon />}
          onClick={() => setAddDialogOpen(true)}
        >
          {t('tracker:workspace.actions.add')}
        </Button>
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
                  {t('tracker:workspace.error.retry')}
                </Button>
              )}
            >
              <AlertTitle>{t('tracker:workspace.error.title')}</AlertTitle>
              {t('tracker:workspace.error.desc')}
            </Alert>
          </Box>
        )}
        {status === 'ready' && workspaces.length === 0 && (
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
            <WorkspacesOutlinedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              {t('tracker:workspace.empty.title')}
            </Typography>
            <Typography variant="body2" color="text.secondary" align="center">
              {t('tracker:workspace.empty.desc')}
            </Typography>
            <Button variant="contained" startIcon={<AddOutlinedIcon />} onClick={() => setAddDialogOpen(true)} sx={{ mt: 1 }}>
              {t('tracker:workspace.actions.add')}
            </Button>
          </Box>
        )}
        {status === 'ready' && workspaces.length > 0 && displayed.length === 0 && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <Typography variant="body2" color="text.secondary">
              {t('tracker:workspace.empty.noMatch')}
            </Typography>
          </Box>
        )}
        {status === 'ready' && displayed.length > 0 && (
          <Box
            sx={{
              p: 2,
              display: 'grid',
              gap: 2,
              // 响应式 1-2 列：窄屏 1 列，宽屏 2 列。
              gridTemplateColumns: {
                xs: '1fr',
                sm: 'repeat(2, 1fr)',
              },
              alignItems: 'start',
            }}
          >
            {displayed.map(ws => (
              <WorkspaceCard
                key={ws.id}
                ws={ws}
                onSelect={onSelect}
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
        <WorkspaceDialog
          onClose={() => setAddDialogOpen(false)}
          onCreated={handleCreated}
        />
      )}

      {editTarget && (
        <WorkspaceDialog
          workspace={editTarget}
          onClose={() => setEditTarget(null)}
          onCreated={handleCreated}
          onUpdated={handleUpdated}
        />
      )}

      <Dialog open={deleteTarget !== null} onClose={deleting ? undefined : () => setDeleteTarget(null)}>
        <DialogTitle>{t('tracker:workspace.delete.title')}</DialogTitle>
        <DialogContent>
          <Typography>{t('tracker:workspace.delete.confirmMsg', { name: deleteTarget?.name ?? '' })}</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDeleteTarget(null)} disabled={deleting}>
            {t('tracker:workspace.delete.cancel')}
          </Button>
          <Button color="error" variant="contained" onClick={handleConfirmDelete} disabled={deleting}>
            {t('tracker:workspace.delete.confirm')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

// 单卡片：整卡可点击进入（onSelect）；Header 放编辑/删除图标（stopPropagation 避免触发进入）；
// Content 放 slug/描述/更新时间。height:100% + flex column 保证网格内同行卡片等高。
interface WorkspaceCardProps {
  ws: WorkspaceModel;
  onSelect: (ws: WorkspaceModel) => void;
  onEdit: (ws: WorkspaceModel) => void;
  onDelete: (ws: WorkspaceModel) => void;
}

function WorkspaceCard({ ws, onSelect, onEdit, onDelete }: WorkspaceCardProps) {
  const { t } = useTranslation();
  const hasDescription = ws.description.trim().length > 0;

  return (
    <Card
      variant="outlined"
      onClick={() => onSelect(ws)}
      sx={{ height: '100%', display: 'flex', flexDirection: 'column', cursor: 'pointer' }}
    >
      <CardHeader
        title={ws.name}
        slotProps={{ title: { fontWeight: 600, noWrap: true } }}
        action={(
          <Box sx={{ display: 'flex', gap: 0.5 }}>
            <IconButton
              size="small"
              onClick={(e) => {
                e.stopPropagation();
                onEdit(ws);
              }}
              aria-label={t('tracker:workspace.card.edit')}
            >
              <EditOutlinedIcon />
            </IconButton>
            <IconButton
              size="small"
              onClick={(e) => {
                e.stopPropagation();
                onDelete(ws);
              }}
              aria-label={t('tracker:workspace.card.delete')}
            >
              <DeleteOutlinedIcon />
            </IconButton>
          </Box>
        )}
        sx={{ '& .MuiCardHeader-action': { alignSelf: 'center', mt: 0 } }}
      />
      <Divider />
      <CardContent sx={{ flex: 1 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mb: 1 }}>
          <TagOutlinedIcon sx={{ fontSize: '0.95rem', color: 'text.disabled' }} />
          <Chip size="small" variant="outlined" label={ws.slug} sx={{ ...truncateSx, maxWidth: '100%' }} />
        </Box>
        <Typography
          variant="body2"
          color={hasDescription ? 'text.secondary' : 'text.disabled'}
          sx={{ mb: 1, minHeight: '1.5em', ...truncateSx }}
          title={hasDescription ? ws.description : undefined}
        >
          {hasDescription ? ws.description : t('tracker:workspace.card.noDescription')}
        </Typography>
        <Typography variant="caption" color="text.disabled" title={formatDate(ws.updatedAt, 'YYYY-MM-DD HH:mm:ss')}>
          {t('tracker:workspace.card.updatedAtLabel')} {formatRelativeTime(new Date(ws.updatedAt).getTime(), t, 'tracker')}
        </Typography>
      </CardContent>
    </Card>
  );
}

export default WorkspacesPage;
