import type { Priority, ProjectIssueResponseData, WorkspaceLabelModel, WorkspaceProjectModel } from '@src/services';
import type { ProjectStateView } from '@src/state/tracker';
import { CloseOutlined as CloseOutlinedIcon, DeleteOutlined as DeleteOutlinedIcon } from '@mui/icons-material';
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Drawer,
  IconButton,
  TextField,
  Typography,
} from '@mui/material';
import { DatePicker, LocalizationProvider } from '@mui/x-date-pickers';
import { AdapterDayjs } from '@mui/x-date-pickers/AdapterDayjs';
import { WorkspaceLabelService } from '@src/services';
import { formatDate } from '@src/shared/time';
import { useToast } from '@src/shared/useToast';
import { useCreateProjectIssue, useDeleteProjectIssue, useUpdateProjectIssue } from '@src/state/tracker';
import dayjs from 'dayjs';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import IssueBranchField from './IssueBranchField';
import MarkdownEditor from './MarkdownEditor/MarkdownEditor';
import PrioritySelect from './PrioritySelect';
import ProjectStateSelect from './ProjectStateSelect';
import WorkspaceLabelManagerDialog from './WorkspaceLabelManagerDialog';
import WorkspaceLabelSelect from './WorkspaceLabelSelect';
import 'dayjs/locale/zh-cn';

interface ProjectIssueDrawerProps {
  mode: 'create' | 'edit';
  workspaceProject: WorkspaceProjectModel;
  projectStates: ProjectStateView[];
  projectIssue?: ProjectIssueResponseData; // edit 模式必传
  // create 模式预选状态（如分组头"+"快捷新建时传入该组首个状态）。
  initialStateId?: number;
  // create 模式新建子 issue 时传入父 issue：顶部展示只读父信息条，提交时带 parentId，并预填父状态。
  parentIssue?: ProjectIssueResponseData;
  onClose: () => void;
  onCreated?: (projectIssue: ProjectIssueResponseData) => void;
  onUpdated?: (projectIssue: ProjectIssueResponseData) => void;
  onDeleted?: (issueId: number) => void;
}

// Issue 抽屉（create/edit 共用）。所有字段（含 labels）本地态，点保存/创建一次性提交，
// 成功后关闭抽屉 + 父级刷新列表；失败 drawer 内弹 error toast（成功 toast 由父级统一弹）。
// mode 决定：初值来源、提交 API、头部标题/元信息/删除按钮显隐。
// create + parentIssue：新建子 issue（顶部只读父信息条 + parentId）。
function ProjectIssueDrawer({ mode, workspaceProject, projectStates, projectIssue, initialStateId, parentIssue, onClose, onCreated, onUpdated, onDeleted }: ProjectIssueDrawerProps) {
  const { t, i18n } = useTranslation();
  const isZh = i18n.language?.toLowerCase().startsWith('zh') ?? false;
  const isCreateSub = mode === 'create' && !!parentIssue;
  const createProjectIssue = useCreateProjectIssue(workspaceProject.id);
  const updateProjectIssue = useUpdateProjectIssue(workspaceProject.id);
  const deleteProjectIssue = useDeleteProjectIssue(workspaceProject.id);
  // 属性字段本地态（挂载即按 mode/projectIssue 初始化，每次打开新挂载，无需 reset effect）。
  const defaultStateId = projectStates.find(s => s.isDefault === 'Y')?.id ?? projectStates[0]?.id ?? 0;
  const [name, setName] = useState(projectIssue?.name ?? '');
  const [description, setDescription] = useState(projectIssue?.description ?? '');
  const [stateId, setStateId] = useState(projectIssue?.stateId ?? initialStateId ?? parentIssue?.stateId ?? defaultStateId);
  const [priority, setPriority] = useState<Priority>(projectIssue?.priority ?? 'none');
  const [startDate, setStartDate] = useState(projectIssue?.startDate ?? '');
  const [targetDate, setTargetDate] = useState(projectIssue?.targetDate ?? '');
  const [labels, setLabels] = useState<WorkspaceLabelModel[]>(projectIssue?.labels ?? []);
  // 关联分支：localRepositoryId 逻辑指向项目关联仓库，repositoryBranch 为分支名（freeSolo 可手输）。
  const [localRepositoryId, setLocalRepositoryId] = useState(projectIssue?.localRepositoryId ?? 0);
  const [repositoryBranch, setRepositoryBranch] = useState(projectIssue?.repositoryBranch ?? '');
  const [wsLabels, setWsLabels] = useState<WorkspaceLabelModel[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [managerOpen, setManagerOpen] = useState(false);
  const { show: showToast, snack } = useToast();

  const loadWsLabels = useCallback(async () => {
    try {
      const data = await WorkspaceLabelService.getList({ workspaceId: workspaceProject.workspaceId });
      setWsLabels(data);
      // 标签管理里若有删除，剔除本地已选 labels 中已不存在的。
      setLabels(prev => prev.filter(l => data.some(w => w.id === l.id)));
    } catch {
      // 标签选项加载失败不阻塞编辑。
    }
  }, [workspaceProject.workspaceId]);

  useEffect(() => {
    void loadWsLabels();
  }, [loadWsLabels]);

  // labels 是否相对原值变化（id 集合比较，覆盖增/删/替换）。
  const labelsDirty = (() => {
    const cur = new Set(labels.map(l => l.id));
    const orig = new Set((projectIssue?.labels ?? []).map(l => l.id));
    if (cur.size !== orig.size) {
      return true;
    }
    for (const id of cur) {
      if (!orig.has(id)) {
        return true;
      }
    }
    return false;
  })();

  const dirty = mode === 'edit' && !!projectIssue && (
    name !== projectIssue.name
    || description !== projectIssue.description
    || stateId !== projectIssue.stateId
    || priority !== projectIssue.priority
    || startDate !== projectIssue.startDate
    || targetDate !== projectIssue.targetDate
    || localRepositoryId !== (projectIssue.localRepositoryId ?? 0)
    || repositoryBranch !== (projectIssue.repositoryBranch ?? '')
    || labelsDirty
  );

  const canSubmit = mode === 'create' ? name.trim().length > 0 : !!dirty;

  // labels 本地切换（不发请求）：create/edit 统一，提交时随 create/update 一起发。
  const handleToggleLabel = useCallback((labelId: number) => {
    setLabels((prev) => {
      if (prev.some(l => l.id === labelId)) {
        return prev.filter(l => l.id !== labelId);
      }
      const found = wsLabels.find(w => w.id === labelId);
      return found ? [...prev, found] : prev;
    });
  }, [wsLabels]);

  // IssueBranchField 受控回调：仓库或分支任一变化都同步本地态（提交统一在 handleSave）。
  const handleBranchChange = useCallback((repoId: number, branch: string) => {
    setLocalRepositoryId(repoId);
    setRepositoryBranch(branch);
  }, []);

  const handleSave = async () => {
    setSubmitting(true);
    try {
      if (mode === 'create') {
        const created = await createProjectIssue.mutateAsync({
          projectId: workspaceProject.id,
          workspaceId: workspaceProject.workspaceId,
          name: name.trim(),
          description,
          priority,
          startDate,
          targetDate,
          stateId,
          labelIds: labels.map(l => l.id),
          localRepositoryId,
          repositoryBranch,
          // 新建子 issue 带父 id（顶级创建不传，走后端默认 0）。
          ...(parentIssue ? { parentId: parentIssue.id } : {}),
        });
        onCreated?.(created);
        onClose();
      } else if (mode === 'edit') {
        const updated = await updateProjectIssue.mutateAsync({
          id: projectIssue!.id,
          name: name.trim(),
          description,
          stateId,
          priority,
          startDate,
          targetDate,
          labelIds: labels.map(l => l.id),
          localRepositoryId,
          repositoryBranch,
        });
        onUpdated?.(updated);
        onClose();
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(
        mode === 'create'
          ? t('tracker:projectIssue.toast.createFailed', { message: msg })
          : t('tracker:projectIssue.toast.updateFailed', { message: msg }),
        'error',
      );
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!projectIssue) {
      return;
    }
    setDeleting(true);
    try {
      await deleteProjectIssue.mutateAsync(projectIssue.id);
      onDeleted?.(projectIssue.id);
      onClose();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:projectIssue.toast.deleteFailed', { message: msg }), 'error');
    } finally {
      setDeleting(false);
      setDeleteOpen(false);
    }
  };

  // 头部标题：edit 显示 #id 名称；create-sub 显示"新建子 Issue"；其余 create 显示"新建 Issue"。
  const title = mode === 'edit'
    ? `#${projectIssue?.id} ${projectIssue?.name ?? ''}`
    : isCreateSub
      ? t('tracker:projectIssue.create.subTitle')
      : t('tracker:projectIssue.create.title');

  return (
    <Drawer anchor="right" open onClose={onClose} sx={{ '& .MuiDrawer-paper': { width: { xs: '100%', sm: '60%' } } }}>
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* 头部 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }} noWrap>
            {title}
          </Typography>
          <IconButton size="small" onClick={onClose} aria-label={t('tracker:projectIssue.detail.close')}>
            <CloseOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 内容 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: 2, display: 'flex', flexDirection: 'column', gap: 2 }}>
          {/* 新建子 issue：顶部只读父信息条（不可修改） */}
          {isCreateSub && parentIssue && (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, px: 1.5, py: 1, borderRadius: 1, bgcolor: 'action.hover' }}>
              <Typography variant="caption" color="text.secondary">{t('tracker:projectIssue.detail.parentIssue')}</Typography>
              <Typography variant="body2" sx={{ fontWeight: 600 }}>#{parentIssue.id}</Typography>
              <Typography variant="body2" color="text.secondary" sx={{ ...({ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' } as const) }}>
                {parentIssue.name}
              </Typography>
            </Box>
          )}
          <TextField
            label={t('tracker:projectIssue.detail.name')}
            value={name}
            onChange={e => setName(e.target.value)}
            fullWidth
            size="small"
            disabled={submitting || deleting}
            slotProps={{ htmlInput: { maxLength: 255 } }}
          />
          <Box>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
              {t('tracker:projectIssue.detail.description')}
            </Typography>
            <MarkdownEditor
              value={description}
              onChange={setDescription}
              placeholder={t('tracker:projectIssue.markdownEditor.placeholder')}
              disabled={submitting || deleting}
            />
          </Box>
          <LocalizationProvider dateAdapter={AdapterDayjs} adapterLocale={isZh ? 'zh-cn' : 'en'}>
            <Box sx={{ display: 'grid', gridTemplateColumns: '80px 1fr', gap: 1.5, alignItems: 'center' }}>
              <Typography variant="body2" color="text.secondary">{t('tracker:projectIssue.detail.state')}</Typography>
              <ProjectStateSelect value={stateId} projectStates={projectStates} onChange={setStateId} disabled={submitting || deleting} />
              <Typography variant="body2" color="text.secondary">{t('tracker:projectIssue.detail.priority')}</Typography>
              <PrioritySelect value={priority} onChange={setPriority} disabled={submitting || deleting} />
              <Typography variant="body2" color="text.secondary">{t('tracker:projectIssue.detail.startDate')}</Typography>
              <DatePicker
                format="YYYY-MM-DD"
                value={startDate ? dayjs(startDate) : null}
                onChange={v => setStartDate(v ? v.format('YYYY-MM-DD') : '')}
                disabled={submitting || deleting}
                slotProps={{ textField: { size: 'small', fullWidth: true } }}
              />
              <Typography variant="body2" color="text.secondary">{t('tracker:projectIssue.detail.targetDate')}</Typography>
              <DatePicker
                format="YYYY-MM-DD"
                value={targetDate ? dayjs(targetDate) : null}
                onChange={v => setTargetDate(v ? v.format('YYYY-MM-DD') : '')}
                disabled={submitting || deleting}
                slotProps={{ textField: { size: 'small', fullWidth: true } }}
              />
            </Box>
          </LocalizationProvider>
          <WorkspaceLabelSelect
            issueLabels={labels}
            options={wsLabels}
            onToggle={handleToggleLabel}
            onOpenManager={() => setManagerOpen(true)}
            disabled={submitting || deleting}
          />
          <IssueBranchField
            localRepositoryIds={workspaceProject.localRepositoryIds ?? []}
            localRepositoryId={localRepositoryId}
            repositoryBranch={repositoryBranch}
            onChange={handleBranchChange}
            disabled={submitting || deleting}
          />
          {/* 元信息（仅 edit） */}
          {mode === 'edit' && projectIssue && (
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 2, mt: 1 }}>
              <Typography variant="caption" color="text.disabled">
                {t('tracker:projectIssue.detail.createdAt')} {formatDate(projectIssue.createdAt, 'YYYY-MM-DD HH:mm')}
              </Typography>
              {projectIssue.completedAt && (
                <Typography variant="caption" color="text.disabled">
                  {t('tracker:projectIssue.detail.completedAt')} {formatDate(projectIssue.completedAt, 'YYYY-MM-DD HH:mm')}
                </Typography>
              )}
            </Box>
          )}
        </Box>

        {/* 底部操作栏：edit 删除+取消+保存；create 取消+创建 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderTop: 1, borderColor: 'divider' }}>
          {mode === 'edit' && (
            <Button
              color="error"
              size="small"
              startIcon={<DeleteOutlinedIcon />}
              onClick={() => setDeleteOpen(true)}
              disabled={submitting || deleting}
            >
              {t('tracker:projectIssue.detail.delete')}
            </Button>
          )}
          <Box sx={{ flex: 1 }} />
          <Button color="inherit" onClick={onClose} disabled={submitting || deleting}>
            {t('tracker:projectIssue.create.cancel')}
          </Button>
          <Button variant="contained" onClick={handleSave} disabled={!canSubmit || submitting}>
            {mode === 'create' ? t('tracker:projectIssue.create.confirm') : t('tracker:projectIssue.detail.save')}
          </Button>
        </Box>
      </Box>

      {managerOpen && (
        <WorkspaceLabelManagerDialog
          workspaceId={workspaceProject.workspaceId}
          labels={wsLabels}
          onClose={() => setManagerOpen(false)}
          onChanged={loadWsLabels}
        />
      )}

      {mode === 'edit' && (
        <Dialog open={deleteOpen} onClose={deleting ? undefined : () => setDeleteOpen(false)}>
          <DialogTitle>{t('tracker:projectIssue.detail.deleteTitle')}</DialogTitle>
          <DialogContent>
            <Typography>{t('tracker:projectIssue.detail.deleteConfirmMsg', { name: projectIssue?.name ?? '' })}</Typography>
          </DialogContent>
          <DialogActions>
            <Button color="inherit" onClick={() => setDeleteOpen(false)} disabled={deleting}>
              {t('tracker:projectIssue.detail.cancel')}
            </Button>
            <Button color="error" variant="contained" onClick={handleDelete} disabled={deleting}>
              {t('tracker:projectIssue.detail.delete')}
            </Button>
          </DialogActions>
        </Dialog>
      )}

      {snack}
    </Drawer>
  );
}

export default ProjectIssueDrawer;
