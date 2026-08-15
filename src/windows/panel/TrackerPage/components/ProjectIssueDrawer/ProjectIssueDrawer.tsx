import type { Priority, ProjectIssueResponseData, WorkspaceLabelModel, WorkspaceProjectModel } from '@src/services';
import type { StateCode } from '@src/state/tracker';
import { CloseOutlined as CloseOutlinedIcon, DeleteOutlined as DeleteOutlinedIcon, DeveloperModeOutlined as DeveloperModeOutlinedIcon } from '@mui/icons-material';
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  TextField,
  Typography,
} from '@mui/material';
import { DatePicker, LocalizationProvider } from '@mui/x-date-pickers';
import { AdapterDayjs } from '@mui/x-date-pickers/AdapterDayjs';
import { WorkspaceLabelService } from '@src/services';
import ResizableDrawer from '@src/shared/ResizableDrawer';
import { formatDate } from '@src/shared/time';
import { useToast } from '@src/shared/useToast';
import { STATE_CODE_DEFAULT, useCreateProjectIssue, useDeleteProjectIssue, useUpdateProjectIssue } from '@src/state/tracker';
import dayjs from 'dayjs';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate as useRouterNavigate } from 'react-router-dom';
import IssueBranchField from './IssueBranchField';
import MarkdownEditor from './MarkdownEditor/MarkdownEditor';
import PrioritySelect from './PrioritySelect';
import ProjectStateSelect from './ProjectStateSelect';
import WorkspaceLabelManagerDrawer from './WorkspaceLabelManagerDrawer';
import WorkspaceLabelSelect from './WorkspaceLabelSelect';
import 'dayjs/locale/zh-cn';

interface ProjectIssueDrawerProps {
  mode: 'create' | 'edit';
  workspaceProject: WorkspaceProjectModel;
  projectIssue?: ProjectIssueResponseData; // edit 模式必传
  // create 模式预选状态（如分组头"+"快捷新建时传入该分组状态）。
  initialStateCode?: StateCode;
  // create 模式新建子 issue 时传入父 issue：顶部展示只读父信息条，提交时带 parentId，并预填父状态。
  parentIssue?: ProjectIssueResponseData;
  onClose: () => void;
  onCreated?: (projectIssue: ProjectIssueResponseData) => void;
  onUpdated?: (projectIssue: ProjectIssueResponseData) => void;
  onDeleted?: (issueId: string) => void;
}

// Issue 抽屉（create/edit 共用）。所有字段（含 labels）本地态，点保存/创建一次性提交，
// 成功后关闭抽屉 + 父级刷新列表；失败 drawer 内弹 error toast（成功 toast 由父级统一弹）。
// mode 决定：初值来源、提交 API、头部标题/元信息/删除按钮显隐。
// create + parentIssue：新建子 issue（顶部只读父信息条 + parentId）。
function ProjectIssueDrawer({ mode, workspaceProject, projectIssue, initialStateCode, parentIssue, onClose, onCreated, onUpdated, onDeleted }: ProjectIssueDrawerProps) {
  const { t, i18n } = useTranslation();
  const routerNavigate = useRouterNavigate();
  const isZh = i18n.language?.toLowerCase().startsWith('zh') ?? false;
  const isCreateSub = mode === 'create' && !!parentIssue;
  // 子 issue（create-child 或 edit-child）：顶部显示「所属父任务」只读字段。
  // 父任务名来自 parentIssue（create-child 传入 / edit-child 由 ProjectIssueList 解析后传入）；解析失败回退父 id 尾段。
  const isChild = isCreateSub || (mode === 'edit' && !!projectIssue?.parentId);
  const parentTaskLabel = parentIssue
    ? `${parentIssue.name}`
    : (projectIssue?.parentId ? `…${projectIssue.parentId.slice(-8)}` : '');
  const createProjectIssue = useCreateProjectIssue(workspaceProject.id);
  const updateProjectIssue = useUpdateProjectIssue(workspaceProject.id);
  const deleteProjectIssue = useDeleteProjectIssue(workspaceProject.id);
  // 属性字段本地态（挂载即按 mode/projectIssue 初始化，每次打开新挂载，无需 reset effect）。
  const [name, setName] = useState(projectIssue?.name ?? '');
  const [description, setDescription] = useState(projectIssue?.description ?? '');
  const [stateCode, setStateCode] = useState<StateCode>(projectIssue?.stateCode ?? initialStateCode ?? parentIssue?.stateCode ?? STATE_CODE_DEFAULT);
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
  const [dialogDeleteOpen, setDialogDeleteOpen] = useState(false);
  const [drawerManagerOpen, setDrawerManagerOpen] = useState(false);
  // F1「进入开发」可用条件（按钮始终展示，不满足时 disabled + title 提示原因）：
  //  issue 当前状态为 IN_PROGRESS（进行中）才可进入开发工作台。
  const canEnterDev = mode === 'edit' && !!projectIssue && projectIssue.stateCode === 'IN_PROGRESS';
  // 禁用原因（首个不满足条件，按钮 title hover 提示；空串=可启用）。
  const enterDevDisabledReason = !projectIssue
    ? '请先保存 issue'
    : projectIssue.stateCode !== 'IN_PROGRESS'
      ? '仅进行中状态可进入开发'
      : '';
  const handleEnterDev = () => {
    if (!projectIssue) {
      return;
    }
    onClose();
    routerNavigate(`/devWorkbench?pid=${projectIssue.projectId}&iid=${projectIssue.id}`);
  };
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
    || stateCode !== projectIssue.stateCode
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
          stateCode,
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
          stateCode,
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
      setDialogDeleteOpen(false);
    }
  };

  // 头部标题：edit 显示 id 尾 8 位 + 名称（uuid 过长，与卡片/工作台同款截断）；create-sub 显示"新建子 Issue"；其余 create 显示"新建 Issue"。
  const title = mode === 'edit'
    ? `…${projectIssue?.id.slice(-8)} ${projectIssue?.name ?? ''}`
    : isCreateSub
      ? t('tracker:projectIssue.create.subTitle')
      : t('tracker:projectIssue.create.title');

  return (
    <ResizableDrawer open onClose={onClose} defaultWidthPct={60}>
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
          {/* 所属项目（只读，create+edit 均显）；子 issue 额外显「所属父任务」 */}
          <TextField
            label={t('tracker:projectIssue.detail.belongingProject')}
            value={workspaceProject.name}
            fullWidth
            slotProps={{ input: { readOnly: true } }}
            variant="filled"
          />
          {isChild && (
            <TextField
              label={t('tracker:projectIssue.detail.belongingParentTask')}
              value={parentTaskLabel}
              fullWidth
              slotProps={{ input: { readOnly: true } }}
              variant="filled"
            />
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
              <ProjectStateSelect value={stateCode} onChange={setStateCode} disabled={submitting || deleting} />
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
            onOpenManager={() => setDrawerManagerOpen(true)}
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

        {/* 底部操作栏：一律左对齐（edit 删除+取消+保存；create 取消+创建） */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderTop: 1, borderColor: 'divider' }}>
          {mode === 'edit' && (
            <Button
              size="small"
              startIcon={<DeveloperModeOutlinedIcon />}
              onClick={handleEnterDev}
              disabled={!canEnterDev || submitting || deleting}
              title={!canEnterDev ? enterDevDisabledReason : undefined}
            >
              {t('panel:devWorkbench.enterDev')}
            </Button>
          )}
          {mode === 'edit' && (
            <Button
              color="error"
              size="small"
              startIcon={<DeleteOutlinedIcon />}
              onClick={() => setDialogDeleteOpen(true)}
              disabled={submitting || deleting}
            >
              {t('tracker:projectIssue.detail.delete')}
            </Button>
          )}
          <Button color="inherit" onClick={onClose} disabled={submitting || deleting}>
            {t('tracker:projectIssue.create.cancel')}
          </Button>
          <Button variant="contained" onClick={handleSave} disabled={!canSubmit || submitting}>
            {mode === 'create' ? t('tracker:projectIssue.create.confirm') : t('tracker:projectIssue.detail.save')}
          </Button>
        </Box>
      </Box>

      {drawerManagerOpen && (
        <WorkspaceLabelManagerDrawer
          workspaceId={workspaceProject.workspaceId}
          labels={wsLabels}
          onClose={() => setDrawerManagerOpen(false)}
          onChanged={loadWsLabels}
        />
      )}

      {mode === 'edit' && (
        <Dialog open={dialogDeleteOpen} onClose={deleting ? undefined : () => setDialogDeleteOpen(false)}>
          <DialogTitle>{t('tracker:projectIssue.detail.deleteTitle')}</DialogTitle>
          <DialogContent>
            <Typography>{t('tracker:projectIssue.detail.deleteConfirmMsg', { name: projectIssue?.name ?? '' })}</Typography>
          </DialogContent>
          <DialogActions>
            <Button color="inherit" onClick={() => setDialogDeleteOpen(false)} disabled={deleting}>
              {t('tracker:projectIssue.detail.cancel')}
            </Button>
            <Button color="error" variant="contained" onClick={handleDelete} disabled={deleting}>
              {t('tracker:projectIssue.detail.delete')}
            </Button>
          </DialogActions>
        </Dialog>
      )}

      {snack}
    </ResizableDrawer>
  );
}

export default ProjectIssueDrawer;
