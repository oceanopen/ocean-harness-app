import type { WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import {
  AddOutlined as AddOutlinedIcon,
  Autorenew as AutorenewIcon,
  DeleteOutlined as DeleteOutlinedIcon,
  EditOutlined as EditOutlinedIcon,
  FolderOutlined as FolderOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import { useToast } from '@src/shared/useToast';
import { useDeleteWorkspaceProject, useTrackerStore, useWorkspaceProjects } from '@src/state/tracker';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import WorkspaceProjectDialog from './WorkspaceProjectDialog';

const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

interface ProjectListProps {
  workspace: WorkspaceModel;
}

// 项目列表（嵌于 tracker 三栏壳的左栏，宽 260）。
// 列表走 useWorkspaceProjects(workspaceId)；选中态读写 tracker store（与命令面板/右栏共享）；
// 删除走 mutation（删当前选中项目时清空 store 已在 mutation 内处理）。本地不再持有列表/选中 state。
function WorkspaceProjectList({ workspace }: ProjectListProps) {
  const { t } = useTranslation();
  const { data: workspaceProjects = [], isLoading, isError, isFetching, refetch } = useWorkspaceProjects(workspace.id);
  const deleteWorkspaceProject = useDeleteWorkspaceProject(workspace.id);
  const selectedProjectId = useTrackerStore(s => s.selectedWorkspaceProject?.id ?? null);
  const selectWorkspaceProject = useTrackerStore(s => s.selectWorkspaceProject);
  const { show: showToast, snack } = useToast();
  const [searchName, setSearchName] = useState('');
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<WorkspaceProjectModel | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<WorkspaceProjectModel | null>(null);
  const [deleting, setDeleting] = useState(false);

  // 客户端模糊过滤 + 兜底排序（id DESC，新建在前）。
  const displayed = useMemo(() => {
    const q = searchName.trim().toLowerCase();
    const filtered = workspaceProjects.filter(p => !q || p.name.toLowerCase().includes(q));
    return [...filtered].sort((a, b) => b.id - a.id);
  }, [workspaceProjects, searchName]);

  // 创建/更新成功：mutation 内部已 invalidate（列表自动刷新），回调仅弹 toast。
  const handleCreated = useCallback((p: WorkspaceProjectModel) => {
    showToast(t('tracker:workspaceProject.toast.created', { name: p.name }), 'success');
  }, [t, showToast]);

  const handleUpdated = useCallback((p: WorkspaceProjectModel) => {
    showToast(t('tracker:workspaceProject.toast.updated', { name: p.name }), 'success');
  }, [t, showToast]);

  const handleConfirmDelete = useCallback(async () => {
    if (!deleteTarget) {
      return;
    }
    setDeleting(true);
    try {
      await deleteWorkspaceProject.mutateAsync(deleteTarget.id);
      // 删除的正是当前选中项目：清空 store 选中已在 useDeleteWorkspaceProject 的 onSuccess 内处理。
      showToast(t('tracker:workspaceProject.toast.deleted'), 'success');
      setDeleteTarget(null);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:workspaceProject.toast.deleteFailed', { message: msg }), 'error');
    } finally {
      setDeleting(false);
    }
  }, [deleteTarget, deleteWorkspaceProject, t, showToast]);

  const ready = !isLoading && !isError;

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 紧凑工具栏：搜索 + 新建 */}
      <Box
        sx={{
          p: 1.5,
          borderBottom: 1,
          borderColor: 'divider',
          display: 'flex',
          alignItems: 'center',
          gap: 1,
        }}
      >
        <TextField
          size="small"
          placeholder={t('tracker:workspaceProject.search')}
          value={searchName}
          onChange={e => setSearchName(e.target.value)}
          sx={{ flexGrow: 1, minWidth: 60 }}
        />
        <Tooltip title={t('tracker:workspaceProject.actions.add')}>
          <IconButton
            size="small"
            color="primary"
            onClick={() => setAddDialogOpen(true)}
            aria-label={t('tracker:workspaceProject.actions.add')}
          >
            <AddOutlinedIcon />
          </IconButton>
        </Tooltip>
        <IconButton
          size="small"
          onClick={() => void refetch()}
          disabled={isFetching}
          aria-label={t('tracker:workspaceProject.actions.refresh')}
        >
          <AutorenewIcon
            sx={{
              'animation': isFetching ? 'spin 0.8s linear infinite' : undefined,
              '@keyframes spin': {
                from: { transform: 'rotate(0deg)' },
                to: { transform: 'rotate(360deg)' },
              },
            }}
          />
        </IconButton>
      </Box>

      {/* 内容区 */}
      <Box sx={{ flex: 1, overflow: 'auto' }}>
        {isLoading && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <CircularProgress />
          </Box>
        )}
        {isError && (
          <Box sx={{ p: 1.5 }}>
            <Alert
              severity="error"
              action={(
                <Button color="inherit" size="small" onClick={() => refetch()}>
                  {t('tracker:workspaceProject.error.retry')}
                </Button>
              )}
            >
              <AlertTitle>{t('tracker:workspaceProject.error.title')}</AlertTitle>
              {t('tracker:workspaceProject.error.desc')}
            </Alert>
          </Box>
        )}
        {ready && workspaceProjects.length === 0 && (
          <Box
            sx={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
              gap: 1,
              px: 2,
              py: 3,
            }}
          >
            <FolderOutlinedIcon sx={{ fontSize: 36, color: 'text.secondary' }} />
            <Typography variant="body2" sx={{ fontWeight: 600 }}>
              {t('tracker:workspaceProject.empty.title')}
            </Typography>
            <Typography variant="caption" color="text.secondary" align="center">
              {t('tracker:workspaceProject.empty.desc')}
            </Typography>
            <Button
              variant="contained"
              size="small"
              startIcon={<AddOutlinedIcon />}
              onClick={() => setAddDialogOpen(true)}
              sx={{ mt: 0.5 }}
            >
              {t('tracker:workspaceProject.actions.add')}
            </Button>
          </Box>
        )}
        {ready && workspaceProjects.length > 0 && displayed.length === 0 && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <Typography variant="body2" color="text.secondary">
              {t('tracker:workspaceProject.empty.noMatch')}
            </Typography>
          </Box>
        )}
        {ready && displayed.length > 0 && (
          <Box sx={{ py: 0.5 }}>
            {displayed.map(p => (
              <WorkspaceProjectRow
                key={p.id}
                workspaceProject={p}
                selected={p.id === selectedProjectId}
                onSelect={selectWorkspaceProject}
                onEdit={setEditTarget}
                onDelete={setDeleteTarget}
              />
            ))}
          </Box>
        )}
      </Box>

      {snack}

      {addDialogOpen && (
        <WorkspaceProjectDialog
          workspaceId={workspace.id}
          onClose={() => setAddDialogOpen(false)}
          onCreated={handleCreated}
        />
      )}

      {editTarget && (
        <WorkspaceProjectDialog
          workspaceId={workspace.id}
          workspaceProject={editTarget}
          onClose={() => setEditTarget(null)}
          onCreated={handleCreated}
          onUpdated={handleUpdated}
        />
      )}

      <Dialog open={deleteTarget !== null} onClose={deleting ? undefined : () => setDeleteTarget(null)}>
        <DialogTitle>{t('tracker:workspaceProject.delete.title')}</DialogTitle>
        <DialogContent>
          <Typography>{t('tracker:workspaceProject.delete.confirmMsg', { name: deleteTarget?.name ?? '' })}</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDeleteTarget(null)} disabled={deleting}>
            {t('tracker:workspaceProject.delete.cancel')}
          </Button>
          <Button color="error" variant="contained" onClick={handleConfirmDelete} disabled={deleting}>
            {t('tracker:workspaceProject.delete.confirm')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

// 单行：整行可点击选中（onSelect）；左侧 emoji（空则兜底图标）+ 名称/描述；右侧编辑/删除图标
// （stopPropagation 避免触发选中）。选中行用左侧主色边条 + action.selected 底色标识。
interface ProjectRowProps {
  workspaceProject: WorkspaceProjectModel;
  selected: boolean;
  onSelect: (p: WorkspaceProjectModel) => void;
  onEdit: (p: WorkspaceProjectModel) => void;
  onDelete: (p: WorkspaceProjectModel) => void;
}

function WorkspaceProjectRow({ workspaceProject, selected, onSelect, onEdit, onDelete }: ProjectRowProps) {
  const { t } = useTranslation();
  const hasDescription = workspaceProject.description.trim().length > 0;
  const emoji = workspaceProject.emoji.trim();

  return (
    <Box
      onClick={() => onSelect(workspaceProject)}
      sx={[
        {
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          mx: 0.5,
          my: 0.25,
          px: 1,
          py: 0.75,
          cursor: 'pointer',
          borderRadius: 1,
          borderLeft: 3,
          borderColor: selected ? 'primary.main' : 'transparent',
          bgcolor: selected ? 'action.selected' : 'transparent',
        },
        // hover 单独成对象：'&:hover' 必须引号，与普通键分离以符合 quote-props consistent。
        { '&:hover': { bgcolor: selected ? 'action.selected' : 'action.hover' } },
      ]}
    >
      <Box sx={{ width: 22, flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        {emoji
          ? (
              <Typography component="span" sx={{ fontSize: '1.1rem', lineHeight: 1 }}>
                {emoji}
              </Typography>
            )
          : (
              <FolderOutlinedIcon sx={{ fontSize: '1.1rem', color: 'text.disabled' }} />
            )}
      </Box>
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <Typography variant="body2" sx={{ fontWeight: 600, ...truncateSx }} title={workspaceProject.name}>
          {workspaceProject.name}
        </Typography>
        {hasDescription && (
          <Typography variant="caption" component="div" color="text.secondary" sx={truncateSx} title={workspaceProject.description}>
            {workspaceProject.description}
          </Typography>
        )}
      </Box>
      <Box sx={{ display: 'flex', flexShrink: 0 }}>
        <IconButton
          size="small"
          onClick={(e) => {
            e.stopPropagation();
            onEdit(workspaceProject);
          }}
          aria-label={t('tracker:workspaceProject.card.edit')}
        >
          <EditOutlinedIcon />
        </IconButton>
        <IconButton
          size="small"
          onClick={(e) => {
            e.stopPropagation();
            onDelete(workspaceProject);
          }}
          aria-label={t('tracker:workspaceProject.card.delete')}
        >
          <DeleteOutlinedIcon />
        </IconButton>
      </Box>
    </Box>
  );
}

export default WorkspaceProjectList;
