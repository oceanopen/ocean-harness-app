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
  TextField,
  Typography,
} from '@mui/material';
import { formatDate, formatRelativeTime } from '@src/shared/time';
import { useToast } from '@src/shared/useToast';
import { useDeleteWorkspace, useWorkspaces } from '@src/state/tracker';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import WorkspaceDialog from './WorkspaceDialog';

const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

interface WorkspacesPageProps {
  onSelect: (ws: WorkspaceModel) => void;
}

// 工作空间管理页（tracker 窗口未选中工作空间时的全屏视图）。
// 列表数据走 useWorkspaces()（与命令面板二级页共享缓存）；增删改走 mutation，内部自动 invalidate，
// 故本地不再持有列表 state、不再手写三态机/load/useEffect/patch。
function WorkspacesPage({ onSelect }: WorkspacesPageProps) {
  const { t } = useTranslation();
  const { data: workspaces = [], isLoading, isError, refetch } = useWorkspaces();
  const deleteWs = useDeleteWorkspace();
  const { show: showToast, snack } = useToast();
  const [searchName, setSearchName] = useState('');
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<WorkspaceModel | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<WorkspaceModel | null>(null);
  const [deleting, setDeleting] = useState(false);

  // 客户端模糊过滤 + 兜底排序（id DESC，新建在前）。
  const displayed = useMemo(() => {
    const q = searchName.trim().toLowerCase();
    const filtered = workspaces.filter(w => !q || w.name.toLowerCase().includes(q));
    return [...filtered].sort((a, b) => b.id - a.id);
  }, [workspaces, searchName]);

  // 创建/更新成功：mutation 内部已 invalidate（列表自动刷新），回调仅弹 toast。
  const handleCreated = useCallback((ws: WorkspaceModel) => {
    showToast(t('tracker:workspace.toast.created', { name: ws.name }), 'success');
  }, [t, showToast]);

  const handleUpdated = useCallback((ws: WorkspaceModel) => {
    showToast(t('tracker:workspace.toast.updated', { name: ws.name }), 'success');
  }, [t, showToast]);

  const handleConfirmDelete = useCallback(async () => {
    if (!deleteTarget) {
      return;
    }
    setDeleting(true);
    try {
      await deleteWs.mutateAsync(deleteTarget.id);
      showToast(t('tracker:workspace.toast.deleted'), 'success');
      setDeleteTarget(null);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:workspace.toast.deleteFailed', { message: msg }), 'error');
    } finally {
      setDeleting(false);
    }
  }, [deleteTarget, deleteWs, t, showToast]);

  const ready = !isLoading && !isError;

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
        {isLoading && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <CircularProgress />
          </Box>
        )}
        {isError && (
          <Box sx={{ p: 2 }}>
            <Alert
              severity="error"
              action={(
                <Button color="inherit" size="small" onClick={() => refetch()}>
                  {t('tracker:workspace.error.retry')}
                </Button>
              )}
            >
              <AlertTitle>{t('tracker:workspace.error.title')}</AlertTitle>
              {t('tracker:workspace.error.desc')}
            </Alert>
          </Box>
        )}
        {ready && workspaces.length === 0 && (
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
        {ready && workspaces.length > 0 && displayed.length === 0 && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <Typography variant="body2" color="text.secondary">
              {t('tracker:workspace.empty.noMatch')}
            </Typography>
          </Box>
        )}
        {ready && displayed.length > 0 && (
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

      {snack}

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
