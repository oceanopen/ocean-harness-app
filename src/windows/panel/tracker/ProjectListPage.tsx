import type { WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import {
  AddOutlined as AddOutlinedIcon,
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
  Snackbar,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import { WorkspaceProjectService } from '@src/services';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ProjectDialog from './components/ProjectDialog';

type LoadStatus = 'loading' | 'ready' | 'error';

// 浮层 toast 严重级别（成功用 success，失败用 error）。
type ToastSeverity = 'success' | 'error';

const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

// WorkspaceProjectModel 类型与请求封装已迁移至 @src/services（WorkspaceProjectService）。

interface ProjectListPageProps {
  workspace: WorkspaceModel;
  // 当前选中项目 id（受控，由 TrackerPage 持有），用于行高亮。
  selectedId: number | null;
  // 行点击选中上抛；删除当前选中项目时回传 null 通知父级清空。
  onSelect: (p: WorkspaceProjectModel | null) => void;
}

// 项目列表页（嵌于 tracker 三栏壳的左栏，宽 260）。
// 三态机 + 紧凑工具栏(搜索+新建) + 可滚动列表行 + toast，照搬 WorkspacesPage 范式，
// 仅把"卡片网格"适配为侧栏紧凑列表行（emoji + 名称 + 描述截断一行 + 选中高亮）。
// 数据刷新用接口返回值直接 setState（仅本页操作变更，无后台 watcher，故不引入事件）。
// 选中态受控上抛：行点击 → onSelect(project)；由 TrackerPage 决定右栏渲染哪个项目的 issue。
function ProjectListPage({ workspace, selectedId, onSelect }: ProjectListPageProps) {
  const { t } = useTranslation();
  const [status, setStatus] = useState<LoadStatus>('loading');
  const [projects, setProjects] = useState<WorkspaceProjectModel[]>([]);
  // toast：保留最近一次内容，toastOpen 控制显隐（退出动画期间内容不闪烁）。
  const [toast, setToast] = useState<{ text: string; severity: ToastSeverity }>({ text: '', severity: 'success' });
  const [toastOpen, setToastOpen] = useState(false);
  const [searchName, setSearchName] = useState('');
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<WorkspaceProjectModel | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<WorkspaceProjectModel | null>(null);
  const [deleting, setDeleting] = useState(false);

  const showToast = useCallback((text: string, severity: ToastSeverity) => {
    setToast({ text, severity });
    setToastOpen(true);
  }, []);

  const load = useCallback(async () => {
    setStatus('loading');
    try {
      const data = await WorkspaceProjectService.getList({ workspaceId: workspace.id });
      setProjects(data);
      setStatus('ready');
    } catch {
      setStatus('error');
    }
  }, [workspace.id]);

  // 挂载时加载一次（workspace 切换时本组件随父级重挂载，自然重新加载）。
  useEffect(() => {
    void load();
  }, [load]);

  // 客户端模糊过滤 + 兜底排序（id DESC，新建在前），保证增删改后无需重新拉取即有序。
  const displayed = useMemo(() => {
    const q = searchName.trim().toLowerCase();
    const filtered = projects.filter(p => !q || p.name.toLowerCase().includes(q));
    return [...filtered].sort((a, b) => b.id - a.id);
  }, [projects, searchName]);

  const handleCreated = useCallback((p: WorkspaceProjectModel) => {
    setProjects(prev => [...prev, p]);
    showToast(t('tracker:project.toast.created', { name: p.name }), 'success');
  }, [t, showToast]);

  const handleUpdated = useCallback((p: WorkspaceProjectModel) => {
    setProjects(prev => prev.map(x => (x.id === p.id ? p : x)));
    showToast(t('tracker:project.toast.updated', { name: p.name }), 'success');
  }, [t, showToast]);

  const handleConfirmDelete = useCallback(async () => {
    if (!deleteTarget) {
      return;
    }
    setDeleting(true);
    try {
      await WorkspaceProjectService.delete({ id: deleteTarget.id });
      setProjects(prev => prev.filter(x => x.id !== deleteTarget.id));
      // 删除的正是当前选中项目：通知父级清空选中，避免右栏指向已删项目。
      if (deleteTarget.id === selectedId) {
        onSelect(null);
      }
      showToast(t('tracker:project.toast.deleted'), 'success');
      setDeleteTarget(null);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:project.toast.deleteFailed', { message: msg }), 'error');
    } finally {
      setDeleting(false);
    }
  }, [deleteTarget, selectedId, onSelect, t, showToast]);

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
          placeholder={t('tracker:project.search')}
          value={searchName}
          onChange={e => setSearchName(e.target.value)}
          sx={{ flexGrow: 1, minWidth: 60 }}
        />
        <Tooltip title={t('tracker:project.actions.add')}>
          <IconButton
            size="small"
            color="primary"
            onClick={() => setAddDialogOpen(true)}
            aria-label={t('tracker:project.actions.add')}
          >
            <AddOutlinedIcon />
          </IconButton>
        </Tooltip>
      </Box>

      {/* 内容区 */}
      <Box sx={{ flex: 1, overflow: 'auto' }}>
        {status === 'loading' && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <CircularProgress />
          </Box>
        )}
        {status === 'error' && (
          <Box sx={{ p: 1.5 }}>
            <Alert
              severity="error"
              action={(
                <Button color="inherit" size="small" onClick={load}>
                  {t('tracker:project.error.retry')}
                </Button>
              )}
            >
              <AlertTitle>{t('tracker:project.error.title')}</AlertTitle>
              {t('tracker:project.error.desc')}
            </Alert>
          </Box>
        )}
        {status === 'ready' && projects.length === 0 && (
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
              {t('tracker:project.empty.title')}
            </Typography>
            <Typography variant="caption" color="text.secondary" align="center">
              {t('tracker:project.empty.desc')}
            </Typography>
            <Button
              variant="contained"
              size="small"
              startIcon={<AddOutlinedIcon />}
              onClick={() => setAddDialogOpen(true)}
              sx={{ mt: 0.5 }}
            >
              {t('tracker:project.actions.add')}
            </Button>
          </Box>
        )}
        {status === 'ready' && projects.length > 0 && displayed.length === 0 && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <Typography variant="body2" color="text.secondary">
              {t('tracker:project.empty.noMatch')}
            </Typography>
          </Box>
        )}
        {status === 'ready' && displayed.length > 0 && (
          <Box sx={{ py: 0.5 }}>
            {displayed.map(p => (
              <ProjectRow
                key={p.id}
                project={p}
                selected={p.id === selectedId}
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
        <ProjectDialog
          workspaceId={workspace.id}
          onClose={() => setAddDialogOpen(false)}
          onCreated={handleCreated}
        />
      )}

      {editTarget && (
        <ProjectDialog
          workspaceId={workspace.id}
          project={editTarget}
          onClose={() => setEditTarget(null)}
          onCreated={handleCreated}
          onUpdated={handleUpdated}
        />
      )}

      <Dialog open={deleteTarget !== null} onClose={deleting ? undefined : () => setDeleteTarget(null)}>
        <DialogTitle>{t('tracker:project.delete.title')}</DialogTitle>
        <DialogContent>
          <Typography>{t('tracker:project.delete.confirmMsg', { name: deleteTarget?.name ?? '' })}</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDeleteTarget(null)} disabled={deleting}>
            {t('tracker:project.delete.cancel')}
          </Button>
          <Button color="error" variant="contained" onClick={handleConfirmDelete} disabled={deleting}>
            {t('tracker:project.delete.confirm')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

// 单行：整行可点击选中（onSelect）；左侧 emoji（空则兜底图标）+ 名称/描述；右侧编辑/删除图标
// （stopPropagation 避免触发选中）。选中行用左侧主色边条 + action.selected 底色标识。
interface ProjectRowProps {
  project: WorkspaceProjectModel;
  selected: boolean;
  onSelect: (p: WorkspaceProjectModel) => void;
  onEdit: (p: WorkspaceProjectModel) => void;
  onDelete: (p: WorkspaceProjectModel) => void;
}

function ProjectRow({ project, selected, onSelect, onEdit, onDelete }: ProjectRowProps) {
  const { t } = useTranslation();
  const hasDescription = project.description.trim().length > 0;
  const emoji = project.emoji.trim();

  return (
    <Box
      onClick={() => onSelect(project)}
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
        <Typography variant="body2" sx={{ fontWeight: 600, ...truncateSx }} title={project.name}>
          {project.name}
        </Typography>
        {hasDescription && (
          <Typography variant="caption" component="div" color="text.secondary" sx={truncateSx} title={project.description}>
            {project.description}
          </Typography>
        )}
      </Box>
      <Box sx={{ display: 'flex', flexShrink: 0 }}>
        <IconButton
          size="small"
          onClick={(e) => {
            e.stopPropagation();
            onEdit(project);
          }}
          aria-label={t('tracker:project.card.edit')}
        >
          <EditOutlinedIcon />
        </IconButton>
        <IconButton
          size="small"
          onClick={(e) => {
            e.stopPropagation();
            onDelete(project);
          }}
          aria-label={t('tracker:project.card.delete')}
        >
          <DeleteOutlinedIcon />
        </IconButton>
      </Box>
    </Box>
  );
}

export default ProjectListPage;
