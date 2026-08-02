import type { WorkspaceProjectModel } from '@src/services';
import { EmojiEmotionsOutlined as EmojiEmotionsOutlinedIcon } from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
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
import { useCreateWorkspaceProject, useUpdateWorkspaceProject } from '@src/state/tracker';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

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

function WorkspaceProjectDialog({ workspaceId, onClose, onCreated, onUpdated, workspaceProject }: WorkspaceProjectDialogProps) {
  const { t } = useTranslation();
  const isEdit = !!workspaceProject;
  const createWorkspaceProject = useCreateWorkspaceProject(workspaceId);
  const updateWorkspaceProject = useUpdateWorkspaceProject(workspaceId);
  const [name, setName] = useState(workspaceProject?.name ?? '');
  const [emoji, setEmoji] = useState(workspaceProject?.emoji ?? '');
  const [description, setDescription] = useState(workspaceProject?.description ?? '');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // emoji 选择浮层锚点（null=关闭；点按钮设为输入框根节点，点表情/外部关闭清空）。
  const [emojiPickerAnchor, setEmojiPickerAnchor] = useState<HTMLElement | null>(null);
  // emoji 输入框根节点 ref：作为选择浮层锚点，使浮层在输入框下方展开（而非按钮下方）。
  const textFieldRef = useRef<HTMLDivElement | null>(null);

  const canSubmit = name.trim().length > 0 && !submitting;

  const handleConfirm = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const payload = {
        name: name.trim(),
        emoji: emoji.trim(),
        description: description.trim(),
      };
      if (isEdit && workspaceProject) {
        const updated = await updateWorkspaceProject.mutateAsync({ id: workspaceProject.id, ...payload });
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
