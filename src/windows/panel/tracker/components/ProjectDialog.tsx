import type { Project } from '../ProjectListPage';
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  TextField,
} from '@mui/material';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { apiPost } from '../api';

// 新建/编辑项目弹窗。
// 传入 project 时为编辑模式：标题改为"编辑项目"、ID 只读展示、字段反显、提交调用 update；
// 不传则为新建模式，行为不变。
//
// 由父组件按需挂载（{open && <ProjectDialog/>}）：每次打开都是全新 useState 初值，
// 无需重置 effect；关闭即卸载。
//
// 与 WorkspaceDialog 的差异：项目无 slug（去 slugify/slugTouched 整套逻辑），改为 emoji 字段；
// create 带 workspaceId、update 不带（对齐后端 ProjectCreate/UpdateRequest）。
interface ProjectDialogProps {
  workspaceId: number;
  onClose: () => void;
  onCreated: (p: Project) => void;
  onUpdated?: (p: Project) => void;
  project?: Project;
}

// 描述/emoji 最大字数（与后端 binding max=500 / max=20 对齐）。
const DESCRIPTION_MAX = 500;
const EMOJI_MAX = 20;

function ProjectDialog({ workspaceId, onClose, onCreated, onUpdated, project }: ProjectDialogProps) {
  const { t } = useTranslation();
  const isEdit = !!project;
  const [name, setName] = useState(project?.name ?? '');
  const [emoji, setEmoji] = useState(project?.emoji ?? '');
  const [description, setDescription] = useState(project?.description ?? '');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
      if (isEdit && project) {
        const updated = await apiPost<Project>('/api/tracker/project/update', {
          id: project.id,
          ...payload,
        });
        onUpdated?.(updated);
      } else {
        const created = await apiPost<Project>('/api/tracker/project/create', {
          workspaceId,
          ...payload,
        });
        onCreated(created);
      }
      onClose();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setError(isEdit ? t('tracker:project.toast.updateFailed', { message: msg }) : t('tracker:project.toast.createFailed', { message: msg }));
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
      <DialogTitle>{isEdit ? t('tracker:project.edit.title') : t('tracker:project.add.title')}</DialogTitle>
      <DialogContent>
        <Box sx={{ mt: 0.5, display: 'flex', flexDirection: 'column', gap: 2 }}>
          {isEdit && (
            <TextField
              label={t('tracker:project.edit.idLabel')}
              value={project!.id}
              fullWidth
              slotProps={{ input: { readOnly: true } }}
              variant="filled"
            />
          )}
          <TextField
            label={t('tracker:project.add.name')}
            placeholder={t('tracker:project.add.namePlaceholder')}
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
            label={t('tracker:project.add.emoji')}
            placeholder={t('tracker:project.add.emojiPlaceholder')}
            value={emoji}
            onChange={(e) => {
              setEmoji(e.target.value);
              setError(null);
            }}
            fullWidth
            disabled={submitting}
            helperText={t('tracker:project.add.emojiHint')}
            slotProps={{ htmlInput: { maxLength: EMOJI_MAX } }}
          />
          <TextField
            label={t('tracker:project.add.description')}
            placeholder={t('tracker:project.add.descriptionPlaceholder')}
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
          {t('tracker:project.add.cancel')}
        </Button>
        <Button variant="contained" onClick={handleConfirm} disabled={!canSubmit}>
          {isEdit ? t('tracker:project.edit.confirm') : t('tracker:project.add.confirm')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

export default ProjectDialog;
