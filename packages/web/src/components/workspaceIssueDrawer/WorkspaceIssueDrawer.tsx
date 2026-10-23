import type { IssueRepositoryBranchModel, WorkspaceIssueResponseData, WorkspaceLaunchSettings, WorkspaceModel, WorkspaceTypeModel } from '@src/services';
import type { AgentCode } from '@src/shared/agentCode';
import type { StateCode } from '@src/state/tracker';
import { CloseOutlined as CloseOutlinedIcon, DeleteOutlined as DeleteOutlinedIcon, DeveloperModeOutlined as DeveloperModeOutlinedIcon, SettingsOutlined as SettingsOutlinedIcon } from '@mui/icons-material';
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
import LaunchSettingsFields from '@src/shared/LaunchSettingsFields';
import ResizableDrawer from '@src/shared/ResizableDrawer';
import { formatDate } from '@src/shared/time';
import { useToast } from '@src/shared/useToast';
import { useEffectiveAcpAgentCode } from '@src/state/agentCatalog';
import { STATE_CODE_DEFAULT, useCreateWorkspaceIssue, useDeleteWorkspaceIssue, useUpdateWorkspaceIssue, useWorkspaceTypes } from '@src/state/tracker';
import dayjs from 'dayjs';
import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate as useRouterNavigate } from 'react-router-dom';
import IssueRepoBranchTable from './IssueRepoBranchTable';
import MarkdownEditor from './MarkdownEditor/MarkdownEditor';
import StateSelect from './StateSelect';
import TypeSelect from './TypeSelect';
import WorkspaceTypeManagerDrawer from './WorkspaceTypeManagerDrawer';
import 'dayjs/locale/zh-cn';

interface WorkspaceIssueDrawerProps {
  mode: 'create' | 'edit';
  workspace: WorkspaceModel;
  workspaceIssue?: WorkspaceIssueResponseData; // edit 模式必传
  // create 模式预选状态（如分组头"+"快捷新建时传入该分组状态）。
  initialStateCode?: StateCode;
  onClose: () => void;
  onCreated?: (workspaceIssue: WorkspaceIssueResponseData) => void;
  onUpdated?: (workspaceIssue: WorkspaceIssueResponseData) => void;
  onDeleted?: (issueId: string) => void;
}

/**
 * Issue 抽屉（create/edit 共用）。所有字段（含类型）本地态，点保存/创建一次性提交，
 * 成功后关闭抽屉 + 父级刷新列表；失败 drawer 内弹 error toast（成功 toast 由父级统一弹）。
 * mode 决定初值来源/提交 API/头部标题与删除按钮显隐。
 */
function WorkspaceIssueDrawer({ mode, workspace, workspaceIssue, initialStateCode, onClose, onCreated, onUpdated, onDeleted }: WorkspaceIssueDrawerProps) {
  const { t, i18n } = useTranslation();
  const routerNavigate = useRouterNavigate();
  const isZh = i18n.language?.toLowerCase().startsWith('zh') ?? false;
  const createWorkspaceIssue = useCreateWorkspaceIssue(workspace.id);
  const updateWorkspaceIssue = useUpdateWorkspaceIssue(workspace.id);
  const deleteWorkspaceIssue = useDeleteWorkspaceIssue(workspace.id);
  // 属性字段本地态（挂载即按 mode/workspaceIssue 初始化，每次打开新挂载，无需 reset effect）。
  const [name, setName] = useState(workspaceIssue?.name ?? '');
  const [description, setDescription] = useState(workspaceIssue?.description ?? '');
  const [stateCode, setStateCode] = useState<StateCode>(workspaceIssue?.stateCode ?? initialStateCode ?? STATE_CODE_DEFAULT);
  const [startDate, setStartDate] = useState(workspaceIssue?.startDate ?? '');
  const [targetDate, setTargetDate] = useState(workspaceIssue?.targetDate ?? '');
  // 类型可选列表（TanStack Query 共享缓存，与类型管理抽屉同源）。
  const wsTypesQuery = useWorkspaceTypes(workspace.id);
  const wsTypes = wsTypesQuery.data;
  const [type, setType] = useState<WorkspaceTypeModel | null>(workspaceIssue?.type ?? null);
  /**
   * 关联仓库+分支列表（多选）：create 默认不预派生仓库行（工作空间关联已下线，归属过滤随 T3.2
   * 项目关联接入）；用户在 table 里增删改后存 override（此后不再用派生值）。
   * edit 按回显数据初始化 override。
   */
  const [repoBranchRowsOverride, setRepoBranchRowsOverride] = useState<IssueRepositoryBranchModel[] | null>(
    mode === 'edit' ? (workspaceIssue?.repositoryBranchList ?? []) : null,
  );
  // 启动设置覆盖（仅 edit）：'' = 跟随工作空间（未覆盖），字段级回落；覆盖档按档带附加字段。
  const [launchModeOverride, setLaunchModeOverride] = useState<'' | NonNullable<WorkspaceLaunchSettings['mode']>>(
    workspaceIssue?.launchSettings?.mode ?? '',
  );
  const [launchAutoCommand, setLaunchAutoCommand] = useState<NonNullable<WorkspaceLaunchSettings['autoCommand']>>(
    workspaceIssue?.launchSettings?.autoCommand ?? 'claude',
  );
  // ACP Agent 覆盖选中态：undefined = 未覆盖（派生首个 enabled），遗留值不静默改写。
  const [launchAgentCode, setLaunchAgentCode] = useState<AgentCode | undefined>(
    workspaceIssue?.launchSettings?.agentCode,
  );
  const effectiveLaunchAgentCode = useEffectiveAcpAgentCode(launchAgentCode);
  const [launchPermissionMode, setLaunchPermissionMode] = useState<NonNullable<WorkspaceLaunchSettings['permissionMode']>>(
    workspaceIssue?.launchSettings?.permissionMode ?? 'bypassPermissions',
  );
  // 覆盖后的启动设置草稿（'' = 未覆盖 → undefined，对齐「省略 = 未覆盖」契约）；dirty 与提交共用。
  const draftLaunchSettings: WorkspaceLaunchSettings | undefined
    = launchModeOverride === ''
      ? undefined
      : launchModeOverride === 'acp'
        ? { mode: launchModeOverride, agentCode: effectiveLaunchAgentCode, permissionMode: launchPermissionMode }
        : launchModeOverride === 'terminal-auto'
          ? { mode: launchModeOverride, autoCommand: launchAutoCommand }
          : { mode: launchModeOverride };
  // create 默认行恒空（不预派生仓库行）；override 在用户增删改后接管。
  const repoBranchRows = repoBranchRowsOverride ?? [];
  // 用户是否已手动操作过类型（create 默认选中「需求」仅作用于未触碰时，避免覆盖用户显式选的「未分类」）。
  const [typeTouched, setTypeTouched] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [dialogDeleteOpen, setDialogDeleteOpen] = useState(false);
  const [drawerManagerOpen, setDrawerManagerOpen] = useState(false);
  /**
   * 「开始处理」可用条件（按钮始终展示，不满足时 disabled + title 提示原因）：
   * 待办池 / 待办 / 进行中三态可直接进入工作台，已完成/已取消不可。
   */
  const canEnterDev = mode === 'edit' && !!workspaceIssue
    && ['BACKLOG', 'TODO', 'IN_PROGRESS'].includes(workspaceIssue.stateCode);
  // 禁用原因（首个不满足条件，按钮 title hover 提示；空串=可启用）。
  const enterDevDisabledReason = !workspaceIssue
    ? '请先保存任务'
    : canEnterDev
      ? ''
      : '已完成/已取消的任务不可开始处理';
  const handleEnterDev = () => {
    if (!workspaceIssue) {
      return;
    }
    onClose();
    routerNavigate(`/devWorkbench?wid=${workspaceIssue.workspaceId}&iid=${workspaceIssue.id}`);
  };
  const { show: showToast, snack } = useToast();

  /**
   * create 且用户未触碰时，类型默认选「需求」（列表无「需求」则取第一项；无类型保持未分类）。
   * 派生值而非 effect 回填：未触碰期间随类型列表异步到达自动生效，触碰后固定为用户显式选择。
   */
  const defaultType = wsTypes?.find(ty => ty.name === t('tracker:workspaceType.defaultRequirement')) ?? wsTypes?.[0] ?? null;
  // 类型列表就绪后本地已选 type 匹配不到（已在类型管理里删除）则视为未分类；未就绪（undefined）时保持原选。
  const typeAlive = !type || wsTypes === undefined || wsTypes.some(ty => ty.id === type.id);
  const currentType = typeTouched || mode !== 'create' ? (typeAlive ? type : null) : defaultType;

  // 类型是否相对原值变化（单值比较；0=未分类）。
  const typeDirty = (currentType?.id ?? 0) !== (workspaceIssue?.typeId ?? 0);

  // 关联仓库+分支列表是否相对原值变化（逐行比较，覆盖增/删/改）。
  const repoBranchDirty = (() => {
    const orig = workspaceIssue?.repositoryBranchList ?? [];
    if (repoBranchRows.length !== orig.length) {
      return true;
    }
    return repoBranchRows.some((row, i) =>
      row.localRepositoryId !== orig[i].localRepositoryId
      || row.repositoryBranch !== orig[i].repositoryBranch);
  })();

  // 启动设置覆盖是否相对原值变化（逐键比较，undefined 归一空串）。
  const launchSettingsDirty = (() => {
    const orig = workspaceIssue?.launchSettings;
    const val = (v: string | undefined) => v ?? '';
    return val(orig?.mode) !== val(draftLaunchSettings?.mode)
      || val(orig?.agentCode) !== val(draftLaunchSettings?.agentCode)
      || val(orig?.autoCommand) !== val(draftLaunchSettings?.autoCommand)
      || val(orig?.permissionMode) !== val(draftLaunchSettings?.permissionMode);
  })();

  const dirty = mode === 'edit' && !!workspaceIssue && (
    name !== workspaceIssue.name
    || description !== workspaceIssue.description
    || stateCode !== workspaceIssue.stateCode
    || startDate !== workspaceIssue.startDate
    || targetDate !== workspaceIssue.targetDate
    || repoBranchDirty
    || typeDirty
    || launchSettingsDirty
  );

  // 存在未选仓库的行（空行）时不可保存（后端也会校验报错，前端先行禁用）。
  const hasEmptyRepoRow = repoBranchRows.some(row => row.localRepositoryId <= 0);

  const canSubmit = (mode === 'create' ? name.trim().length > 0 : !!dirty) && !hasEmptyRepoRow;

  // 类型本地切换（不发请求，0=未分类）：create/edit 统一，提交时随 create/update 一起发。
  const handleTypeChange = useCallback((typeId: number) => {
    setTypeTouched(true);
    setType(wsTypes?.find(ty => ty.id === typeId) ?? null);
  }, [wsTypes]);

  // IssueRepoBranchTable 受控回调：行增删/仓库分支变化都存 override（提交统一在 handleSave）。
  const handleRepoBranchRowsChange = useCallback((rows: IssueRepositoryBranchModel[]) => {
    setRepoBranchRowsOverride(rows);
  }, []);

  const handleSave = async () => {
    setSubmitting(true);
    try {
      if (mode === 'create') {
        const created = await createWorkspaceIssue.mutateAsync({
          workspaceId: workspace.id,
          name: name.trim(),
          description,
          startDate,
          targetDate,
          stateCode,
          typeId: currentType?.id ?? 0,
          repositoryBranchList: repoBranchRows,
        });
        onCreated?.(created);
        onClose();
      } else if (mode === 'edit') {
        const updated = await updateWorkspaceIssue.mutateAsync({
          id: workspaceIssue!.id,
          name: name.trim(),
          description,
          stateCode,
          startDate,
          targetDate,
          typeId: currentType?.id ?? 0,
          repositoryBranchList: repoBranchRows,
          launchSettings: draftLaunchSettings,
        });
        onUpdated?.(updated);
        onClose();
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(
        mode === 'create'
          ? t('tracker:workspaceIssue.toast.createFailed', { message: msg })
          : t('tracker:workspaceIssue.toast.updateFailed', { message: msg }),
        'error',
      );
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!workspaceIssue) {
      return;
    }
    setDeleting(true);
    try {
      await deleteWorkspaceIssue.mutateAsync(workspaceIssue.id);
      onDeleted?.(workspaceIssue.id);
      onClose();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:workspaceIssue.toast.deleteFailed', { message: msg }), 'error');
    } finally {
      setDeleting(false);
      setDialogDeleteOpen(false);
    }
  };

  // 头部标题：edit 仅名称（uuid 由表单只读「任务ID」项承载）；create 显示"新建 Issue"。
  const title = mode === 'edit'
    ? workspaceIssue?.name ?? ''
    : t('tracker:workspaceIssue.create.title');

  return (
    <ResizableDrawer open onClose={onClose} defaultWidthPct={60}>
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* 头部 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', p: '16px', borderBottom: '1px solid', borderColor: 'divider' }}>
          <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }} noWrap>
            {title}
          </Typography>
          <IconButton size="small" onClick={onClose} aria-label={t('tracker:workspaceIssue.detail.close')}>
            <CloseOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 内容 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: '16px', display: 'flex', flexDirection: 'column', gap: '16px' }}>
          {/* 所属工作空间（只读，create+edit 均显） */}
          <TextField
            label={t('tracker:workspaceIssue.detail.belongingWorkspace')}
            value={workspace.name}
            fullWidth
            slotProps={{ input: { readOnly: true } }}
            variant="filled"
          />
          {/* 任务ID（只读，edit 显）：uuid 生成于后端，create 尚无值不渲染。 */}
          {mode === 'edit' && workspaceIssue && (
            <TextField
              label="任务ID"
              value={workspaceIssue.id}
              fullWidth
              slotProps={{ input: { readOnly: true } }}
              variant="filled"
            />
          )}
          <TextField
            label={t('tracker:workspaceIssue.detail.name')}
            value={name}
            onChange={e => setName(e.target.value)}
            fullWidth
            size="small"
            disabled={submitting || deleting}
            slotProps={{ htmlInput: { maxLength: 255 } }}
          />
          <Box>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: '4px' }}>
              {t('tracker:workspaceIssue.detail.description')}
            </Typography>
            <MarkdownEditor
              value={description}
              onChange={setDescription}
              placeholder={t('tracker:workspaceIssue.markdownEditor.placeholder')}
              disabled={submitting || deleting}
            />
          </Box>
          <LocalizationProvider dateAdapter={AdapterDayjs} adapterLocale={isZh ? 'zh-cn' : 'en'}>
            <Box sx={{ display: 'grid', gridTemplateColumns: '80px 1fr', gap: '12px', alignItems: 'center' }}>
              <Typography variant="body2" color="text.secondary">{t('tracker:workspaceIssue.detail.state')}</Typography>
              <StateSelect value={stateCode} onChange={setStateCode} disabled={submitting || deleting} />
              <Typography variant="body2" color="text.secondary">{t('tracker:workspaceIssue.detail.type')}</Typography>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                <TypeSelect value={currentType?.id ?? 0} types={wsTypes ?? []} onChange={handleTypeChange} disabled={submitting || deleting} />
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
              <Typography variant="body2" color="text.secondary">{t('tracker:workspaceIssue.detail.startDate')}</Typography>
              <DatePicker
                format="YYYY-MM-DD"
                value={startDate ? dayjs(startDate) : null}
                onChange={v => setStartDate(v ? v.format('YYYY-MM-DD') : '')}
                disabled={submitting || deleting}
                slotProps={{ textField: { size: 'small', fullWidth: true } }}
              />
              <Typography variant="body2" color="text.secondary">{t('tracker:workspaceIssue.detail.targetDate')}</Typography>
              <DatePicker
                format="YYYY-MM-DD"
                value={targetDate ? dayjs(targetDate) : null}
                onChange={v => setTargetDate(v ? v.format('YYYY-MM-DD') : '')}
                disabled={submitting || deleting}
                slotProps={{ textField: { size: 'small', fullWidth: true } }}
              />
            </Box>
          </LocalizationProvider>
          <IssueRepoBranchTable
            rows={repoBranchRows}
            onChange={handleRepoBranchRowsChange}
            disabled={submitting || deleting}
          />

          {/* 启动设置覆盖（launch_settings，仅 edit 回显/编辑）：未覆盖键字段级回落工作空间。 */}
          {mode === 'edit' && (
            <LaunchSettingsFields
              mode={launchModeOverride}
              onModeChange={setLaunchModeOverride}
              autoCommand={launchAutoCommand}
              onAutoCommandChange={setLaunchAutoCommand}
              agentCode={launchAgentCode}
              onAgentCodeChange={setLaunchAgentCode}
              permissionMode={launchPermissionMode}
              onPermissionModeChange={setLaunchPermissionMode}
              disabled={submitting || deleting}
              showFollowOption
            />
          )}
          {/* 元信息（仅 edit） */}
          {mode === 'edit' && workspaceIssue && (
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: '16px', mt: '8px' }}>
              <Typography variant="caption" color="text.disabled" sx={{ opacity: 0.6 }}>
                {t('tracker:workspaceIssue.detail.createdAt')} {formatDate(workspaceIssue.createdAt, 'YYYY-MM-DD HH:mm')}
              </Typography>
              {workspaceIssue.completedAt && (
                <Typography variant="caption" color="text.disabled" sx={{ opacity: 0.6 }}>
                  {t('tracker:workspaceIssue.detail.completedAt')} {formatDate(workspaceIssue.completedAt, 'YYYY-MM-DD HH:mm')}
                </Typography>
              )}
            </Box>
          )}
        </Box>

        {/* 底部操作栏：一律左对齐（edit 开始处理+删除+取消+保存；create 取消+创建） */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', p: '16px', borderTop: '1px solid', borderColor: 'divider' }}>
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
              {t('tracker:workspaceIssue.detail.delete')}
            </Button>
          )}
          <Button color="inherit" onClick={onClose} disabled={submitting || deleting}>
            {t('tracker:workspaceIssue.create.cancel')}
          </Button>
          <Button variant="contained" onClick={handleSave} disabled={!canSubmit || submitting}>
            {mode === 'create' ? t('tracker:workspaceIssue.create.confirm') : t('tracker:workspaceIssue.detail.save')}
          </Button>
        </Box>
      </Box>

      {drawerManagerOpen && (
        <WorkspaceTypeManagerDrawer
          workspaceId={workspace.id}
          onClose={() => setDrawerManagerOpen(false)}
        />
      )}

      {mode === 'edit' && (
        <Dialog open={dialogDeleteOpen} onClose={deleting ? undefined : () => setDialogDeleteOpen(false)}>
          <DialogTitle>{t('tracker:workspaceIssue.detail.deleteTitle')}</DialogTitle>
          <DialogContent>
            <Typography>{t('tracker:workspaceIssue.detail.deleteConfirmMsg', { name: workspaceIssue?.name ?? '' })}</Typography>
          </DialogContent>
          <DialogActions>
            <Button color="inherit" onClick={() => setDialogDeleteOpen(false)} disabled={deleting}>
              {t('tracker:workspaceIssue.detail.cancel')}
            </Button>
            <Button color="error" variant="contained" onClick={handleDelete} disabled={deleting}>
              {t('tracker:workspaceIssue.detail.delete')}
            </Button>
          </DialogActions>
        </Dialog>
      )}

      {snack}
    </ResizableDrawer>
  );
}

export default WorkspaceIssueDrawer;
