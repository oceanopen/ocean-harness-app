import type { LocalRepositoryModel, ProjectStateItem, WorkspaceProjectModel } from '@src/services';
import { EmojiEmotionsOutlined as EmojiEmotionsOutlinedIcon } from '@mui/icons-material';
import {
  Alert,
  Autocomplete,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  InputAdornment,
  Popover,
  TextField,
  Tooltip,
} from '@mui/material';
import { useLocalRepositories } from '@src/state/localRepositories';
import { trackerKeys, useCreateWorkspaceProject, useProjectStates, useUpdateWorkspaceProject } from '@src/state/tracker';
import { useQueryClient } from '@tanstack/react-query';
import { useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ProjectStateManage from '../ProjectStateManage';

// 新建/编辑项目弹窗。
// 传入 workspaceProject 时为编辑模式：标题改为"编辑项目"、ID 只读展示、字段反显、提交调用 update；
// 不传则为新建模式，行为不变。
//
// 由父组件按需挂载（{open && <WorkspaceProjectDialog/>}）：每次打开都是全新 useState 初值，
// 无需重置 effect；关闭即卸载。
//
// 与 WorkspaceDialog 的差异：项目无 slug（去 slugify/slugTouched 整套逻辑），改为 emoji 字段；
// create 带 workspaceId、update 不带（对齐后端 ProjectCreate/UpdateRequest）。
interface WorkspaceProjectDialogProps {
  workspaceId: number;
  onClose: () => void;
  onCreated: (p: WorkspaceProjectModel) => void;
  onUpdated?: (p: WorkspaceProjectModel) => void;
  workspaceProject?: WorkspaceProjectModel;
}

// 描述/emoji 最大字数（与后端 binding max=500 / max=20 对齐）。
const DESCRIPTION_MAX = 500;
const EMOJI_MAX = 20;

// 项目图标常用 emoji 预设（点选填入，仍可在上方文本框自定义粘贴/清空）。
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

// 新建项目默认状态全量列表（决策#4：开箱自带完整开发步骤条）。
// 引用后端固定目录 StateCatalog（9 项），backlog 项为默认状态；sortOrder 仅占位（保存时按目录重排）。
const DEFAULT_STATES: ProjectStateItem[] = [
  { stateGroupCode: 'backlog', stateCode: 'backlog', sortOrder: 1000, isDefault: 'Y' },
  { stateGroupCode: 'unstarted', stateCode: 'todo', sortOrder: 2000, isDefault: 'N' },
  { stateGroupCode: 'started', stateCode: 'in_progress', sortOrder: 3000, isDefault: 'N' },
  { stateGroupCode: 'started', stateCode: 'wt_init', sortOrder: 4000, isDefault: 'N' },
  { stateGroupCode: 'started', stateCode: 'developing', sortOrder: 5000, isDefault: 'N' },
  { stateGroupCode: 'started', stateCode: 'pr_open', sortOrder: 6000, isDefault: 'N' },
  { stateGroupCode: 'started', stateCode: 'cleanup', sortOrder: 7000, isDefault: 'N' },
  { stateGroupCode: 'completed', stateCode: 'done', sortOrder: 8000, isDefault: 'N' },
  { stateGroupCode: 'cancelled', stateCode: 'cancelled', sortOrder: 9000, isDefault: 'N' },
];

function WorkspaceProjectDialog({ workspaceId, onClose, onCreated, onUpdated, workspaceProject }: WorkspaceProjectDialogProps) {
  const { t } = useTranslation();
  const isEdit = !!workspaceProject;
  const createWorkspaceProject = useCreateWorkspaceProject(workspaceId);
  const updateWorkspaceProject = useUpdateWorkspaceProject(workspaceId);
  // 关联仓库：全量仓库（选项）；编辑模式选中态直接从项目 prop 回显（项目响应自带 localRepositoryIds）。
  const allReposQuery = useLocalRepositories();
  const allRepos: LocalRepositoryModel[] = allReposQuery.data ?? [];
  const [name, setName] = useState(workspaceProject?.name ?? '');
  const [emoji, setEmoji] = useState(workspaceProject?.emoji ?? '');
  const [description, setDescription] = useState(workspaceProject?.description ?? '');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // 选中的关联仓库 id 列表：直接从项目 prop 初始化（无需独立取数/种子化，无回显 bug）。
  const [selectedRepoIds, setSelectedRepoIds] = useState<number[]>(workspaceProject?.localRepositoryIds ?? []);
  // 项目状态全量列表（引用目录）。新建=默认全勾（DEFAULT_STATES）；编辑=回显该项目现有状态。
  // override 派生模式：未改时随 existingStates 自动派生（避免 effect 同步 set），用户改动后用 override 覆写。
  const qc = useQueryClient();
  const projectId = workspaceProject?.id ?? 0;
  const { data: existingStates = [] } = useProjectStates(projectId);
  const baseStates = useMemo<ProjectStateItem[]>(
    () => (isEdit
      ? existingStates.map(s => ({
          stateGroupCode: s.stateGroupCode,
          stateCode: s.stateCode,
          sortOrder: s.sortOrder,
          isDefault: s.isDefault,
        }))
      : DEFAULT_STATES),
    [isEdit, existingStates],
  );
  const [statesOverride, setStatesOverride] = useState<ProjectStateItem[] | null>(null);
  const states = statesOverride ?? baseStates;
  // emoji 选择浮层锚点（null=关闭；点按钮设为输入框根节点，点表情/外部关闭清空）。
  const [emojiPickerAnchor, setEmojiPickerAnchor] = useState<HTMLElement | null>(null);
  // emoji 输入框根节点 ref：作为选择浮层锚点，使浮层在输入框下方展开（而非按钮下方）。
  const textFieldRef = useRef<HTMLDivElement | null>(null);

  const canSubmit = name.trim().length > 0 && !submitting;

  const handleConfirm = async () => {
    setSubmitting(true);
    setError(null);
    try {
      // 关联仓库 + 状态全量列表随项目信息一起保存（后端 create/update 事务内全量写入，无独立增删接口）。
      // create/update mutation 的 onSuccess 已失效 workspaceProjects；状态变更额外失效 projectStates。
      const payload = {
        name: name.trim(),
        emoji: emoji.trim(),
        description: description.trim(),
        localRepositoryIds: selectedRepoIds,
        states,
      };
      if (isEdit && workspaceProject) {
        const updated = await updateWorkspaceProject.mutateAsync({ id: workspaceProject.id, ...payload });
        qc.invalidateQueries({ queryKey: trackerKeys.projectStates(workspaceProject.id) });
        onUpdated?.(updated);
      } else {
        const created = await createWorkspaceProject.mutateAsync({ workspaceId, ...payload });
        onCreated(created);
      }
      onClose();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setError(isEdit ? t('tracker:workspaceProject.toast.updateFailed', { message: msg }) : t('tracker:workspaceProject.toast.createFailed', { message: msg }));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog
      open
      // 提交中禁止背景点击/Esc 关闭，避免半成品状态丢失。
      onClose={submitting ? undefined : onClose}
      fullWidth
      maxWidth="sm"
    >
      <DialogTitle>{isEdit ? t('tracker:workspaceProject.edit.title') : t('tracker:workspaceProject.add.title')}</DialogTitle>
      <DialogContent>
        <Box sx={{ mt: 0.5, display: 'flex', flexDirection: 'column', gap: 2 }}>
          {isEdit && (
            <TextField
              label={t('tracker:workspaceProject.edit.idLabel')}
              value={workspaceProject!.id}
              fullWidth
              slotProps={{ input: { readOnly: true } }}
              variant="filled"
            />
          )}
          <TextField
            label={t('tracker:workspaceProject.add.name')}
            placeholder={t('tracker:workspaceProject.add.namePlaceholder')}
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
            ref={textFieldRef}
            label={t('tracker:workspaceProject.add.emoji')}
            placeholder={t('tracker:workspaceProject.add.emojiPlaceholder')}
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
                    <Tooltip title={t('tracker:workspaceProject.add.emojiPicker')}>
                      <IconButton
                        size="small"
                        edge="end"
                        onClick={() => setEmojiPickerAnchor(textFieldRef.current)}
                        disabled={submitting}
                        aria-label={t('tracker:workspaceProject.add.emojiPicker')}
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
            <Box sx={{ p: 1.5, maxWidth: 320, display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
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
                      'width': 32,
                      'height': 32,
                      'p': 0,
                      'fontSize': '1.1rem',
                      'lineHeight': 1,
                      'border': 2,
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
            label={t('tracker:workspaceProject.add.description')}
            placeholder={t('tracker:workspaceProject.add.descriptionPlaceholder')}
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
            noOptionsText={t('tracker:workspaceProject.add.repositoriesNoRepo')}
            renderValue={(value, getItemProps) =>
              value.map((r, idx) => {
                const { key, ...itemProps } = getItemProps({ index: idx });
                return <Chip key={key} label={r.name} size="small" {...itemProps} />;
              })}
            renderInput={params => (
              <TextField
                {...params}
                label={t('tracker:workspaceProject.add.repositories')}
                placeholder={t('tracker:workspaceProject.add.repositoriesPlaceholder')}
              />
            )}
          />
          <ProjectStateManage states={states} onChange={setStatesOverride} disabled={submitting} />
          {error && <Alert severity="error">{error}</Alert>}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button color="inherit" onClick={onClose} disabled={submitting}>
          {t('tracker:workspaceProject.add.cancel')}
        </Button>
        <Button variant="contained" onClick={handleConfirm} disabled={!canSubmit}>
          {isEdit ? t('tracker:workspaceProject.edit.confirm') : t('tracker:workspaceProject.add.confirm')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

export default WorkspaceProjectDialog;
