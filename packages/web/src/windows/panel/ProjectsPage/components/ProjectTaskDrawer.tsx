import type { IssueRepositoryBranchModel, ProjectModel, ProjectTaskResponseData, ProjectTypeModel, WorkspaceLaunchSettings } from '@src/services';
import type { AgentCode } from '@src/shared/agentCode';
import { CloseOutlined as CloseOutlinedIcon, DeleteOutlined as DeleteOutlinedIcon, SettingsOutlined as SettingsOutlinedIcon } from '@mui/icons-material';
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
import LaunchSettingsFields from '@src/shared/LaunchSettingsFields';
import ResizableDrawer from '@src/shared/ResizableDrawer';
import { useToast } from '@src/shared/useToast';
import { useEffectiveAcpAgentCode } from '@src/state/agentCatalog';
import { useLocalRepositories } from '@src/state/localRepositories';
import { useCreateProjectTask, useDeleteProjectTask, useProjectTypes, useUpdateProjectTask } from '@src/state/tracker';
import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import MarkdownEditor from './MarkdownEditor/MarkdownEditor';
import TaskRepoBranchTable from './TaskRepoBranchTable';
import TypeSelect from './TypeSelect';
import WorkspaceTypeManagerDrawer from './WorkspaceTypeManagerDrawer';

interface ProjectTaskDrawerProps {
  mode: 'create' | 'edit';
  // 宿主项目：所属项目只读展示 + 类型列表/仓库池/提交归属的共同来源。
  project: Pick<ProjectModel, 'id' | 'name' | 'localRepositoryIds'>;
  task?: ProjectTaskResponseData; // edit 模式必传
  onClose: () => void;
  onCreated?: (task: ProjectTaskResponseData) => void;
  onUpdated?: (task: ProjectTaskResponseData) => void;
  onDeleted?: (taskId: string) => void;
}

/**
 * 项目任务抽屉（create/edit 共用，D3 字段族：无状态/无日期字段）。
 * 字段 = 所属项目（只读）/任务ID（编辑只读）/名称/描述（Markdown）/任务类型（+管理入口）/
 * 默认执行模式（任务级覆盖，未覆盖键字段级回落项目）/关联仓库+分支（选项收窄到项目关联集合）。
 * 所有字段本地态，保存一次性提交；删除走两段式确认（先关终端 pane 再删行）。
 */
function ProjectTaskDrawer({ mode, project, task, onClose, onCreated, onUpdated, onDeleted }: ProjectTaskDrawerProps) {
  const { t } = useTranslation();
  const { show: showToast, snack } = useToast();
  const isEdit = mode === 'edit';
  const createTask = useCreateProjectTask(project.id);
  const updateTask = useUpdateProjectTask(project.id);
  const deleteTask = useDeleteProjectTask(project.id);
  // 属性字段本地态（挂载即按 mode/task 初始化，每次打开新挂载，无需 reset effect）。
  const [name, setName] = useState(task?.name ?? '');
  const [description, setDescription] = useState(task?.description ?? '');
  // 类型可选列表（与类型管理抽屉同源缓存）。
  const typesQuery = useProjectTypes(project.id);
  const types = typesQuery.data;
  const [type, setType] = useState<ProjectTypeModel | null>(task?.type ?? null);
  // 关联仓库+分支行（多选）：create 默认空行，用户增删改后存 override；edit 按回显初始化。
  const [repoBranchRowsOverride, setRepoBranchRowsOverride] = useState<IssueRepositoryBranchModel[] | null>(
    isEdit ? (task?.repositoryBranchList ?? []) : null,
  );
  // 仓库池 = 项目关联仓库（选项收窄到项目集合，见 TaskRepoBranchTable）。
  const { data: allRepos = [] } = useLocalRepositories();
  const repoPool = allRepos.filter(r => project.localRepositoryIds.includes(r.id));
  // 启动设置覆盖：'' = 跟随项目（未覆盖），字段级回落；覆盖档按档带附加字段。
  const [launchModeOverride, setLaunchModeOverride] = useState<'' | NonNullable<WorkspaceLaunchSettings['mode']>>(
    task?.launchSettings?.mode ?? '',
  );
  const [launchAutoCommand, setLaunchAutoCommand] = useState<NonNullable<WorkspaceLaunchSettings['autoCommand']>>(
    task?.launchSettings?.autoCommand ?? 'claude',
  );
  // ACP Agent 覆盖选中态：undefined = 未覆盖（派生首个 enabled），遗留值不静默改写。
  const [launchAgentCode, setLaunchAgentCode] = useState<AgentCode | undefined>(task?.launchSettings?.agentCode);
  const effectiveLaunchAgentCode = useEffectiveAcpAgentCode(launchAgentCode);
  // 用户是否已手动操作过类型（create 默认选「需求」仅作用于未触碰时，避免覆盖用户显式选择）。
  const [typeTouched, setTypeTouched] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [dialogDeleteOpen, setDialogDeleteOpen] = useState(false);
  const [drawerManagerOpen, setDrawerManagerOpen] = useState(false);

  const repoBranchRows = repoBranchRowsOverride ?? [];

  /**
   * create 且用户未触碰时，类型默认选「需求」（列表无「需求」则取第一项；无类型保持未分类）。
   * 派生值而非 effect 回填：未触碰期间随类型列表异步到达自动生效，触碰后固定为用户显式选择。
   */
  const defaultType = types?.find(ty => ty.name === t('tracker:workspaceType.defaultRequirement')) ?? types?.[0] ?? null;
  // 类型列表就绪后本地已选 type 匹配不到（已在类型管理里删除）则视为未分类；未就绪（undefined）时保持原选。
  const typeAlive = !type || types === undefined || types.some(ty => ty.id === type.id);
  const currentType = typeTouched || !isEdit ? (typeAlive ? type : null) : defaultType;

  // 类型是否相对原值变化（单值比较；0=未分类）。
  const typeDirty = (currentType?.id ?? 0) !== (task?.typeId ?? 0);

  // 关联仓库+分支列表是否相对原值变化（逐行比较，覆盖增/删/改）。
  const repoBranchDirty = (() => {
    const orig = task?.repositoryBranchList ?? [];
    if (repoBranchRows.length !== orig.length) {
      return true;
    }
    return repoBranchRows.some((row, i) =>
      row.localRepositoryId !== orig[i].localRepositoryId
      || row.repositoryBranch !== orig[i].repositoryBranch);
  })();

  // 覆盖后的启动设置草稿（'' = 未覆盖 → undefined，对齐「省略 = 未覆盖」契约）；dirty 与提交共用。
  const draftLaunchSettings: WorkspaceLaunchSettings | undefined
    = launchModeOverride === ''
      ? undefined
      : launchModeOverride === 'acp'
        ? { mode: launchModeOverride, agentCode: effectiveLaunchAgentCode }
        : launchModeOverride === 'terminal-auto'
          ? { mode: launchModeOverride, autoCommand: launchAutoCommand }
          : { mode: launchModeOverride };

  // 启动设置覆盖是否相对原值变化（逐键比较，undefined 归一空串）。
  const launchSettingsDirty = (() => {
    const orig = task?.launchSettings;
    const val = (v: string | undefined) => v ?? '';
    return val(orig?.mode) !== val(draftLaunchSettings?.mode)
      || val(orig?.agentCode) !== val(draftLaunchSettings?.agentCode)
      || val(orig?.autoCommand) !== val(draftLaunchSettings?.autoCommand);
  })();

  const dirty = isEdit && task != null && (
    name !== task.name
    || description !== task.description
    || repoBranchDirty
    || typeDirty
    || launchSettingsDirty
  );

  // 存在未选仓库的行（空行）时不可保存（后端也会校验报错，前端先行禁用）。
  const hasEmptyRepoRow = repoBranchRows.some(row => row.localRepositoryId <= 0);

  const canSubmit = (isEdit ? dirty : name.trim().length > 0) && !hasEmptyRepoRow;

  // 类型本地切换（不发请求，0=未分类）：create/edit 统一，提交时随 create/update 一起发。
  const handleTypeChange = useCallback((typeId: number) => {
    setTypeTouched(true);
    setType(types?.find(ty => ty.id === typeId) ?? null);
  }, [types]);

  // TaskRepoBranchTable 受控回调：行增删/仓库分支变化都存 override（提交统一在 handleSave）。
  const handleRepoBranchRowsChange = useCallback((rows: IssueRepositoryBranchModel[]) => {
    setRepoBranchRowsOverride(rows);
  }, []);

  const handleSave = async () => {
    setSubmitting(true);
    try {
      if (!isEdit) {
        const created = await createTask.mutateAsync({
          projectId: project.id,
          name: name.trim(),
          description,
          typeId: currentType?.id ?? 0,
          repositoryBranchList: repoBranchRows,
          launchSettings: draftLaunchSettings,
        });
        onCreated?.(created);
      } else if (task != null) {
        const updated = await updateTask.mutateAsync({
          id: task.id,
          name: name.trim(),
          description,
          typeId: currentType?.id ?? 0,
          repositoryBranchList: repoBranchRows,
          launchSettings: draftLaunchSettings,
        });
        onUpdated?.(updated);
      }
      onClose();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(isEdit ? `保存失败：${msg}` : `创建失败：${msg}`, 'error');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (task == null || deleting) {
      return;
    }
    setDeleting(true);
    try {
      await deleteTask.mutateAsync(task.id);
      showToast('任务已删除', 'success');
      onDeleted?.(task.id);
      onClose();
    } catch (e) {
      showToast(`删除失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    } finally {
      setDeleting(false);
      setDialogDeleteOpen(false);
    }
  };

  // 头部标题：edit 仅名称（uuid 由表单只读「任务ID」项承载）；create 显示「新建任务」。
  const title = isEdit ? task?.name ?? '' : '新建任务';

  return (
    <ResizableDrawer open onClose={onClose} defaultWidthPct={60}>
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* 头部 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', p: '16px', borderBottom: '1px solid', borderColor: 'divider' }}>
          <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }} noWrap>
            {title}
          </Typography>
          <IconButton size="small" onClick={onClose} aria-label="关闭">
            <CloseOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 内容 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: '16px', display: 'flex', flexDirection: 'column', gap: '16px' }}>
          {/* 所属项目（只读，create+edit 均显） */}
          <TextField
            label="所属项目"
            value={project.name}
            fullWidth
            slotProps={{ input: { readOnly: true } }}
            variant="filled"
          />
          {/* 任务ID（只读，edit 显）：uuid 生成于后端，create 尚无值不渲染。 */}
          {isEdit && task != null && (
            <TextField
              label="任务ID"
              value={task.id}
              fullWidth
              slotProps={{ input: { readOnly: true } }}
              variant="filled"
            />
          )}
          <TextField
            label="任务名称"
            placeholder="请输入任务名称"
            value={name}
            onChange={e => setName(e.target.value)}
            fullWidth
            size="small"
            disabled={submitting || deleting}
            slotProps={{ htmlInput: { maxLength: 255 } }}
          />
          <Box>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: '4px' }}>
              描述
            </Typography>
            <MarkdownEditor
              value={description}
              onChange={setDescription}
              placeholder={t('tracker:workspaceIssue.markdownEditor.placeholder')}
              disabled={submitting || deleting}
            />
          </Box>
          <Box sx={{ display: 'grid', gridTemplateColumns: '80px 1fr', gap: '12px', alignItems: 'center' }}>
            <Typography variant="body2" color="text.secondary">任务类型</Typography>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
              <TypeSelect value={currentType?.id ?? 0} types={types ?? []} onChange={handleTypeChange} disabled={submitting || deleting} />
              <IconButton
                size="small"
                onClick={() => setDrawerManagerOpen(true)}
                disabled={submitting || deleting}
                aria-label={t('tracker:workspaceType.title')}
                title={t('tracker:workspaceType.title')}
              >
                <SettingsOutlinedIcon fontSize="small" />
              </IconButton>
            </Box>
          </Box>
          <TaskRepoBranchTable
            rows={repoBranchRows}
            onChange={handleRepoBranchRowsChange}
            repoPool={repoPool}
            disabled={submitting || deleting}
          />
          {/* 启动设置覆盖（launch_settings）：未覆盖键字段级回落项目生效值。 */}
          <LaunchSettingsFields
            mode={launchModeOverride}
            onModeChange={setLaunchModeOverride}
            autoCommand={launchAutoCommand}
            onAutoCommandChange={setLaunchAutoCommand}
            agentCode={launchAgentCode}
            onAgentCodeChange={setLaunchAgentCode}
            disabled={submitting || deleting}
            showFollowOption
          />
        </Box>

        {/* 底部操作栏：一律左对齐（edit 删除+取消+保存；create 取消+创建） */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', p: '16px', borderTop: '1px solid', borderColor: 'divider' }}>
          {isEdit && (
            <Button
              color="error"
              size="small"
              startIcon={<DeleteOutlinedIcon />}
              onClick={() => setDialogDeleteOpen(true)}
              disabled={submitting || deleting}
            >
              删除
            </Button>
          )}
          <Button color="inherit" onClick={onClose} disabled={submitting || deleting}>取消</Button>
          <Button variant="contained" onClick={() => void handleSave()} disabled={!canSubmit || submitting}>
            {isEdit ? '保存' : '创建'}
          </Button>
        </Box>
      </Box>

      {drawerManagerOpen && (
        <WorkspaceTypeManagerDrawer
          projectId={project.id}
          onClose={() => setDrawerManagerOpen(false)}
        />
      )}

      {isEdit && (
        <Dialog open={dialogDeleteOpen} onClose={deleting ? undefined : () => setDialogDeleteOpen(false)}>
          <DialogTitle>删除任务</DialogTitle>
          <DialogContent>
            <Typography>确认删除任务「{task?.name ?? ''}」？此操作不可恢复。</Typography>
          </DialogContent>
          <DialogActions>
            <Button color="inherit" onClick={() => setDialogDeleteOpen(false)} disabled={deleting}>取消</Button>
            <Button color="error" variant="contained" onClick={() => void handleDelete()} disabled={deleting}>删除</Button>
          </DialogActions>
        </Dialog>
      )}

      {snack}
    </ResizableDrawer>
  );
}

export default ProjectTaskDrawer;
