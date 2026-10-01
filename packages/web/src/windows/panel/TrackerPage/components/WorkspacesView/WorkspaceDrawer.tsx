import type { WorkspaceLaunchSettings, WorkspaceModel } from '@src/services';
import type { AgentCode } from '@src/shared/agentCode';
import { CloseOutlined as CloseOutlinedIcon, FolderOpen as FolderOpenIcon } from '@mui/icons-material';
import { Alert, Box, Button, IconButton, InputAdornment, TextField, Typography } from '@mui/material';
import LaunchSettingsFields from '@src/shared/LaunchSettingsFields';
import ResizableDrawer from '@src/shared/ResizableDrawer';
import { useEffectiveAcpAgentCode } from '@src/state/agentCatalog';
import { useCreateWorkspace, useUpdateWorkspace } from '@src/state/tracker';
import { open as openDialog } from '@tauri-apps/plugin-dialog';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

// 新建/编辑工作空间抽屉。
// 传入 workspace 时为编辑模式：标题改为"编辑工作空间"、ID 只读展示、字段反显、提交调用 update；
// 不传则为新建模式，行为不变。
// 底部「启动设置」区块（launch_settings）：workspace 单源，无全局回落。启动模式四档——
// none（默认，就绪后出启动方式选择面板）/ 自动打开终端（裸 shell，Agent 经工具条自选）/
// 终端 - 自动启动（「终端 Agent」下拉 = autoCommand）/ ACP 会话（「ACP Agent」=
// agentCode + 执行模式）。新增文案中文直出（仅本文件既有字段走 i18n）。
//
// 由父组件按需挂载（{open && <WorkspaceDrawer/>}）：每次打开都是全新 useState 初值，
// 无需重置 effect；关闭即卸载。
interface WorkspaceDrawerProps {
  onClose: () => void;
  onCreated: (ws: WorkspaceModel) => void;
  onUpdated?: (ws: WorkspaceModel) => void;
  workspace?: WorkspaceModel;
}

// 描述最大字数（与后端 binding max=500 对齐）。
const DESCRIPTION_MAX = 500;

function WorkspaceDrawer({ onClose, onCreated, onUpdated, workspace }: WorkspaceDrawerProps) {
  const { t } = useTranslation();
  const isEdit = !!workspace;
  const createWs = useCreateWorkspace();
  const updateWs = useUpdateWorkspace();
  const [name, setName] = useState(workspace?.name ?? '');
  const [dir, setDir] = useState(workspace?.dir ?? '');
  const [description, setDescription] = useState(workspace?.description ?? '');
  const [launchMode, setLaunchMode] = useState<NonNullable<WorkspaceLaunchSettings['mode']>>(
    workspace?.launchSettings?.mode ?? 'none',
  );
  const [autoCommand, setAutoCommand] = useState<NonNullable<WorkspaceLaunchSettings['autoCommand']>>(
    workspace?.launchSettings?.autoCommand ?? 'claude',
  );
  // ACP Agent 选中态：undefined = 未显式选择（派生首个 enabled），遗留值不静默改写。
  const [agentCode, setAgentCode] = useState<AgentCode | undefined>(workspace?.launchSettings?.agentCode);
  const effectiveAgentCode = useEffectiveAcpAgentCode(agentCode);
  const [permissionMode, setPermissionMode] = useState<NonNullable<WorkspaceLaunchSettings['permissionMode']>>(
    workspace?.launchSettings?.permissionMode ?? 'acceptEdits',
  );
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleBrowse = async () => {
    // directory: true 多选关闭，返回 string | null。
    const selected = await openDialog({ directory: true, multiple: false });
    if (typeof selected === 'string') {
      setDir(selected);
      setError(null);
    }
  };

  const canSubmit = name.trim().length > 0 && dir.trim().length > 0 && !submitting;

  const handleConfirm = async () => {
    setSubmitting(true);
    setError(null);
    try {
      // none / 手动档只带 mode（手动 = 自动打开终端，不自动拉起 Agent，经终端工具条
      // 自行选择）；自动档带 autoCommand（直启目标）；ACP 会话带 agentCode（= 目录
      // 条目 code，未显式选择时为派生默认）+ permissionMode。
      const launchSettings: WorkspaceLaunchSettings
        = launchMode === 'acp'
          ? { mode: launchMode, agentCode: effectiveAgentCode, permissionMode }
          : launchMode === 'terminal-auto'
            ? { mode: launchMode, autoCommand }
            : { mode: launchMode };
      const payload = {
        name: name.trim(),
        dir: dir.trim(),
        description: description.trim(),
        launchSettings,
      };
      if (isEdit && workspace) {
        const updated = await updateWs.mutateAsync({ id: workspace.id, ...payload });
        onUpdated?.(updated);
      } else {
        const created = await createWs.mutateAsync(payload);
        onCreated(created);
      }
      onClose();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setError(isEdit ? `更新失败：${msg}` : `创建失败：${msg}`);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <ResizableDrawer
      open
      // 提交中禁止背景点击/Esc 关闭，避免半成品状态丢失。
      onClose={submitting ? undefined : onClose}
      defaultWidthPct={50}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* 头部 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }} noWrap>
            {isEdit ? t('tracker:workspace.edit.title') : t('tracker:workspace.add.title')}
          </Typography>
          <IconButton size="small" onClick={onClose} disabled={submitting} aria-label={t('tracker:workspace.add.cancel')}>
            <CloseOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 内容 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: 2, display: 'flex', flexDirection: 'column', gap: 2 }}>
          {isEdit && (
            <TextField
              label={t('tracker:workspace.edit.idLabel')}
              value={workspace!.id}
              fullWidth
              slotProps={{ input: { readOnly: true } }}
              variant="filled"
            />
          )}
          <TextField
            label={t('tracker:workspace.add.name')}
            placeholder={t('tracker:workspace.add.namePlaceholder')}
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              setError(null);
            }}
            fullWidth
            autoFocus
            disabled={submitting}
          />
          <TextField
            required
            label={t('tracker:workspace.add.dir')}
            placeholder={t('tracker:workspace.add.dirPlaceholder')}
            value={dir}
            onChange={(e) => {
              setDir(e.target.value);
              setError(null);
            }}
            fullWidth
            disabled={submitting}
            helperText={t('tracker:workspace.add.dirHint')}
            slotProps={{
              input: {
                endAdornment: (
                  <InputAdornment position="end">
                    <IconButton size="small" onClick={handleBrowse} aria-label={t('tracker:workspace.add.dir')} disabled={submitting}>
                      <FolderOpenIcon fontSize="small" />
                    </IconButton>
                  </InputAdornment>
                ),
              },
            }}
          />
          <TextField
            label={t('tracker:workspace.add.description')}
            placeholder={t('tracker:workspace.add.descriptionPlaceholder')}
            value={description}
            onChange={(e) => {
              setDescription(e.target.value);
              setError(null);
            }}
            fullWidth
            multiline
            minRows={3}
            maxRows={5}
            disabled={submitting}
            slotProps={{ htmlInput: { maxLength: DESCRIPTION_MAX } }}
            helperText={`${description.length} / ${DESCRIPTION_MAX}`}
          />

          {/* 启动设置（launch_settings）：workspace 单源，无全局回落；未配置等同手动。 */}
          <LaunchSettingsFields
            mode={launchMode}
            onModeChange={(m) => {
              setLaunchMode(m as NonNullable<WorkspaceLaunchSettings['mode']>);
              setError(null);
            }}
            autoCommand={autoCommand}
            onAutoCommandChange={(v) => {
              setAutoCommand(v);
              setError(null);
            }}
            agentCode={agentCode}
            onAgentCodeChange={(code) => {
              setAgentCode(code);
              setError(null);
            }}
            permissionMode={permissionMode}
            onPermissionModeChange={setPermissionMode}
            disabled={submitting}
          />
          {error && <Alert severity="error">{error}</Alert>}
        </Box>

        {/* 底部操作栏：一律左对齐 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderTop: 1, borderColor: 'divider' }}>
          <Button color="inherit" onClick={onClose} disabled={submitting}>
            {t('tracker:workspace.add.cancel')}
          </Button>
          <Button variant="contained" onClick={handleConfirm} disabled={!canSubmit}>
            {isEdit ? t('tracker:workspace.edit.confirm') : t('tracker:workspace.add.confirm')}
          </Button>
        </Box>
      </Box>
    </ResizableDrawer>
  );
}

export default WorkspaceDrawer;
