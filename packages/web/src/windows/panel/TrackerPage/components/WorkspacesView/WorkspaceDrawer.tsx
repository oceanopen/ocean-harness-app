import type { LocalRepositoryModel, WorkspaceLaunchSettings, WorkspaceModel } from '@src/services';
import type { AgentCode } from '@src/shared/agentCode';
import { CloseOutlined as CloseOutlinedIcon, EmojiEmotionsOutlined as EmojiEmotionsOutlinedIcon, FolderOpen as FolderOpenIcon } from '@mui/icons-material';
import {
  Alert,
  Autocomplete,
  Box,
  Button,
  Chip,
  IconButton,
  InputAdornment,
  Popover,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import LaunchSettingsFields from '@src/shared/LaunchSettingsFields';
import ResizableDrawer from '@src/shared/ResizableDrawer';
import { useEffectiveAcpAgentCode } from '@src/state/agentCatalog';
import { useLocalRepositories } from '@src/state/localRepositories';
import { useCreateWorkspace, useUpdateWorkspace } from '@src/state/tracker';
import { open as openDialog } from '@tauri-apps/plugin-dialog';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

// 新建/编辑工作空间抽屉。
// 传入 workspace 时为编辑模式：标题改为"编辑工作空间"、ID 只读展示、字段反显、提交调用 update；
// 不传则为新建模式，行为不变。
// 字段：基础信息（名称/目录/emoji/描述）+ 关联仓库（多选，随空间一起全量保存）+ 启动设置
// （launch_settings：workspace 单源，无全局回落，未配置等同手动）。
//
// 由父组件按需挂载（{open && <WorkspaceDrawer/>}）：每次打开都是全新 useState 初值，
// 无需重置 effect；关闭即卸载。
interface WorkspaceDrawerProps {
  onClose: () => void;
  onCreated: (ws: WorkspaceModel) => void;
  onUpdated?: (ws: WorkspaceModel) => void;
  workspace?: WorkspaceModel;
}

// 描述/emoji 最大字数（与后端 binding max=500 / max=20 对齐）。
const DESCRIPTION_MAX = 500;
const EMOJI_MAX = 20;

// 工作空间图标常用 emoji 预设（点选填入，仍可在上方文本框自定义粘贴/清空）。
const EMOJI_PRESETS = [
  '🚀',
  '🎯',
  '📦',
  '💡',
  '🔥',
  '⚡',
  '🛠️',
  '📊',
  '🎨',
  '🧩',
  '🔒',
  '🔑',
  '🌐',
  '📱',
  '💻',
  '🐛',
  '✨',
  '🌱',
  '📈',
  '🏗️',
  '🧪',
  '📚',
  '🎮',
  '🎵',
  '🗂️',
  '💼',
  '🏠',
  '⭐',
  '🔔',
  '✅',
  '🏆',
  '💎',
];

function WorkspaceDrawer({ onClose, onCreated, onUpdated, workspace }: WorkspaceDrawerProps) {
  const { t } = useTranslation();
  const isEdit = !!workspace;
  const createWs = useCreateWorkspace();
  const updateWs = useUpdateWorkspace();
  // 关联仓库：全量仓库（选项）；编辑模式选中态直接从空间 prop 回显（空间响应自带 localRepositoryIds）。
  const allReposQuery = useLocalRepositories();
  const allRepos: LocalRepositoryModel[] = allReposQuery.data ?? [];
  const [name, setName] = useState(workspace?.name ?? '');
  const [dir, setDir] = useState(workspace?.dir ?? '');
  const [emoji, setEmoji] = useState(workspace?.emoji ?? '');
  const [description, setDescription] = useState(workspace?.description ?? '');
  const [selectedRepoIds, setSelectedRepoIds] = useState<number[]>(workspace?.localRepositoryIds ?? []);
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
    workspace?.launchSettings?.permissionMode ?? 'bypassPermissions',
  );
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // emoji 选择浮层锚点（null=关闭；点按钮设为输入框根节点，点表情/外部关闭清空）。
  const [emojiPickerAnchor, setEmojiPickerAnchor] = useState<HTMLElement | null>(null);
  // emoji 输入框根节点 ref：作为选择浮层锚点，使浮层在输入框下方展开（而非按钮下方）。
  const textFieldRef = useRef<HTMLDivElement | null>(null);

  const handleBrowse = async () => {
    // directory: true 多选关闭，返回 string | null。
    const selected = await openDialog({ directory: true, multiple: false });
    if (typeof selected === 'string') {
      setDir(selected);
      // 名称未手填时以路径末段为默认值（兼容 / 与 \ 分隔），已填不覆盖。
      if (name.trim() === '') {
        const segment = selected.split(/[\\/]/).filter(Boolean).pop();
        if (segment) {
          setName(segment);
        }
      }
      setError(null);
    }
  };

  const canSubmit = name.trim().length > 0 && dir.trim().length > 0 && !submitting;

  const handleConfirm = async () => {
    setSubmitting(true);
    setError(null);
    try {
      // 关联仓库随空间信息一起保存（后端 create/update 事务内全量写入，无独立增删接口）。
      // none / 手动档只带 mode；自动档带 autoCommand（直启目标）；ACP 会话带 agentCode + permissionMode。
      const launchSettings: WorkspaceLaunchSettings
        = launchMode === 'acp'
          ? { mode: launchMode, agentCode: effectiveAgentCode, permissionMode }
          : launchMode === 'terminal-auto'
            ? { mode: launchMode, autoCommand }
            : { mode: launchMode };
      const payload = {
        name: name.trim(),
        dir: dir.trim(),
        emoji: emoji.trim(),
        description: description.trim(),
        localRepositoryIds: selectedRepoIds,
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
        <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', p: '16px', borderBottom: '1px solid', borderColor: 'divider' }}>
          <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }} noWrap>
            {isEdit ? t('tracker:workspace.edit.title') : t('tracker:workspace.add.title')}
          </Typography>
          <IconButton size="small" onClick={onClose} disabled={submitting} aria-label={t('tracker:workspace.add.cancel')}>
            <CloseOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 内容 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: '16px', display: 'flex', flexDirection: 'column', gap: '16px' }}>
          {isEdit && (
            <TextField
              label="工作空间ID"
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
            ref={textFieldRef}
            label={t('tracker:workspace.add.emoji')}
            placeholder={t('tracker:workspace.add.emojiPlaceholder')}
            value={emoji}
            onChange={(e) => {
              setEmoji(e.target.value);
              setError(null);
            }}
            fullWidth
            disabled={submitting}
            slotProps={{
              htmlInput: { maxLength: EMOJI_MAX },
              input: {
                endAdornment: (
                  <InputAdornment position="end">
                    <Tooltip title={t('tracker:workspace.add.emojiPicker')}>
                      <IconButton
                        size="small"
                        edge="end"
                        onClick={() => setEmojiPickerAnchor(textFieldRef.current)}
                        disabled={submitting}
                        aria-label={t('tracker:workspace.add.emojiPicker')}
                      >
                        <EmojiEmotionsOutlinedIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  </InputAdornment>
                ),
              },
            }}
          />
          {/* emoji 选择浮层：锚点为输入框本身，在输入框下方展开（非按钮下方）；点选填入并关闭 */}
          <Popover
            open={Boolean(emojiPickerAnchor)}
            anchorEl={emojiPickerAnchor}
            onClose={() => setEmojiPickerAnchor(null)}
            anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
          >
            <Box sx={{ p: '12px', maxWidth: '320px', display: 'flex', flexWrap: 'wrap', gap: '4px' }}>
              {EMOJI_PRESETS.map((e) => {
                const selected = emoji === e;
                return (
                  <IconButton
                    key={e}
                    size="small"
                    onClick={() => {
                      setEmoji(e);
                      setError(null);
                      setEmojiPickerAnchor(null);
                    }}
                    disabled={submitting}
                    sx={{
                      'width': '32px',
                      'height': '32px',
                      'p': '0px',
                      'fontSize': '18px',
                      'lineHeight': 1,
                      'border': '2px solid',
                      'borderColor': selected ? 'primary.main' : 'transparent',
                      'bgcolor': selected ? 'action.selected' : 'transparent',
                      '&:hover': { bgcolor: 'action.hover' },
                    }}
                  >
                    {e}
                  </IconButton>
                );
              })}
            </Box>
          </Popover>
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
          <Autocomplete
            multiple
            size="small"
            options={allRepos}
            getOptionLabel={r => r.name}
            isOptionEqualToValue={(o, v) => o.id === v.id}
            value={allRepos.filter(r => selectedRepoIds.includes(r.id))}
            onChange={(_, picked) => {
              setSelectedRepoIds(picked.map(r => r.id));
              setError(null);
            }}
            filterSelectedOptions
            disabled={submitting}
            noOptionsText={t('tracker:workspace.add.repositoriesNoRepo')}
            renderValue={(value, getItemProps) =>
              value.map((r, idx) => {
                const { key, ...itemProps } = getItemProps({ index: idx });
                return <Chip key={key} label={r.name} size="small" {...itemProps} />;
              })}
            renderInput={params => (
              <TextField
                {...params}
                label={t('tracker:workspace.add.repositories')}
                placeholder={t('tracker:workspace.add.repositoriesPlaceholder')}
              />
            )}
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
        <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', p: '16px', borderTop: '1px solid', borderColor: 'divider' }}>
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
