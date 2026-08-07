import type { WorkspaceLabelModel } from '@src/services';
import {
  AddOutlined as AddOutlinedIcon,
  CloseOutlined as CloseOutlinedIcon,
  DeleteOutlined as DeleteOutlinedIcon,
  EditOutlined as EditOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  Drawer,
  IconButton,
  Snackbar,
  TextField,
  Typography,
} from '@mui/material';
import { WorkspaceLabelService } from '@src/services';
import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';

// 预设色板（与 plane 默认状态色系接近），勾选 + 可手填 hex。
const COLOR_PRESETS = [
  '#ef4444',
  '#f59e0b',
  '#eab308',
  '#16a34a',
  '#06b6d4',
  '#3b82f6',
  '#8b5cf6',
  '#ec4899',
  '#64748b',
];

type ToastSeverity = 'success' | 'error';

// workspace 标签管理抽屉（CRUD）：顶部新建/编辑表单（name + 色板/hex + description）+ 已有标签列表。
// 由父组件按需挂载。create 不传 sortOrder（后端 MAX+10000）；update 直接覆盖 color/description；
// delete 级联清 projectIssue 关联。每次变更调 onChanged 让父级重拉。
interface LabelManagerDrawerProps {
  workspaceId: number;
  labels: WorkspaceLabelModel[];
  onClose: () => void;
  onChanged: () => void;
}

function WorkspaceLabelManagerDrawer({ workspaceId, labels, onClose, onChanged }: LabelManagerDrawerProps) {
  const { t } = useTranslation();
  const [editId, setEditId] = useState<number | null>(null);
  const [name, setName] = useState('');
  const [color, setColor] = useState(COLOR_PRESETS[0]);
  const [description, setDescription] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<WorkspaceLabelModel | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [toast, setToast] = useState<{ text: string; severity: ToastSeverity }>({ text: '', severity: 'success' });
  const [toastOpen, setToastOpen] = useState(false);

  const showToast = useCallback((text: string, severity: ToastSeverity) => {
    setToast({ text, severity });
    setToastOpen(true);
  }, []);

  const isEdit = editId !== null;
  const canSubmit = name.trim().length > 0 && !submitting;

  const resetForm = () => {
    setEditId(null);
    setName('');
    setColor(COLOR_PRESETS[0]);
    setDescription('');
    setError(null);
  };

  const startEdit = (l: WorkspaceLabelModel) => {
    setEditId(l.id);
    setName(l.name);
    setColor(l.color || COLOR_PRESETS[0]);
    setDescription(l.description);
    setError(null);
  };

  const handleCreateOrUpdate = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const payload = { name: name.trim(), color: color.trim(), description: description.trim() };
      if (isEdit && editId !== null) {
        await WorkspaceLabelService.update({ id: editId, ...payload });
        showToast(t('tracker:projectIssue.toast.labelUpdated'), 'success');
      } else {
        await WorkspaceLabelService.create({ workspaceId, ...payload });
        showToast(t('tracker:projectIssue.toast.labelCreated', { name: payload.name }), 'success');
      }
      onChanged();
      resetForm();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setError(t('tracker:projectIssue.toast.labelOpFailed', { message: msg }));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) {
      return;
    }
    setDeleting(true);
    try {
      await WorkspaceLabelService.delete({ id: deleteTarget.id });
      showToast(t('tracker:projectIssue.toast.labelDeleted'), 'success');
      if (editId === deleteTarget.id) {
        resetForm();
      }
      setDeleteTarget(null);
      onChanged();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:projectIssue.toast.labelOpFailed', { message: msg }), 'error');
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Drawer
      anchor="right"
      open
      // 提交/删除中禁止背景点击/Esc 关闭，避免半成品状态丢失。
      onClose={submitting || deleting ? undefined : onClose}
      sx={{ '& .MuiDrawer-paper': { width: { xs: '100%', sm: '40%' } } }}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* 头部 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }} noWrap>
            {t('tracker:workspaceLabel.title')}
          </Typography>
          <IconButton size="small" onClick={onClose} disabled={submitting || deleting} aria-label={t('tracker:workspaceLabel.close')}>
            <CloseOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 内容 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: 2, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
          {/* 新建/编辑表单 */}
          <TextField
            label={t('tracker:workspaceLabel.name')}
            placeholder={t('tracker:workspaceLabel.namePlaceholder')}
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              setError(null);
            }}
            fullWidth
            size="small"
            autoFocus
            disabled={submitting}
          />
          <Box>
            <Typography variant="caption" color="text.secondary">{t('tracker:workspaceLabel.color')}</Typography>
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75, mt: 0.5, alignItems: 'center' }}>
              {COLOR_PRESETS.map(c => (
                <Box
                  key={c}
                  onClick={() => setColor(c)}
                  sx={{
                    width: 22,
                    height: 22,
                    borderRadius: '50%',
                    bgcolor: c,
                    cursor: 'pointer',
                    border: color.toLowerCase() === c ? 2 : 0,
                    borderColor: 'text.primary',
                    boxSizing: 'border-box',
                  }}
                />
              ))}
              <TextField
                size="small"
                value={color}
                onChange={(e) => {
                  setColor(e.target.value);
                  setError(null);
                }}
                sx={{ width: 110 }}
                disabled={submitting}
                slotProps={{ htmlInput: { maxLength: 20 } }}
              />
            </Box>
          </Box>
          <TextField
            label={t('tracker:workspaceLabel.description')}
            placeholder={t('tracker:workspaceLabel.descriptionPlaceholder')}
            value={description}
            onChange={(e) => {
              setDescription(e.target.value);
              setError(null);
            }}
            fullWidth
            size="small"
            multiline
            minRows={2}
            maxRows={4}
            disabled={submitting}
            slotProps={{ htmlInput: { maxLength: 500 } }}
          />
          {error && <Alert severity="error">{error}</Alert>}
          <Box sx={{ display: 'flex', gap: 1 }}>
            <Button
              variant="contained"
              size="small"
              startIcon={isEdit ? undefined : <AddOutlinedIcon />}
              onClick={handleCreateOrUpdate}
              disabled={!canSubmit}
            >
              {isEdit ? t('tracker:workspaceLabel.save') : t('tracker:workspaceLabel.create')}
            </Button>
            {isEdit && (
              <Button color="inherit" size="small" onClick={resetForm} disabled={submitting}>
                {t('tracker:workspaceLabel.cancel')}
              </Button>
            )}
          </Box>

          {labels.length > 0 && <Divider sx={{ my: 0.5 }} />}

          {/* 已有标签列表 */}
          {labels.map(l => (
            <Box key={l.id} sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <Box sx={{ width: 12, height: 12, borderRadius: '50%', bgcolor: l.color || 'text.disabled', flexShrink: 0 }} />
              <Typography variant="body2" sx={{ flex: 1, minWidth: 0 }} noWrap>{l.name}</Typography>
              {l.description && <Chip label={l.description} size="small" variant="outlined" sx={{ maxWidth: 160 }} />}
              <IconButton size="small" onClick={() => startEdit(l)} aria-label={t('tracker:workspaceLabel.edit')}>
                <EditOutlinedIcon />
              </IconButton>
              <IconButton size="small" onClick={() => setDeleteTarget(l)} aria-label={t('tracker:workspaceLabel.delete')}>
                <DeleteOutlinedIcon />
              </IconButton>
            </Box>
          ))}
          {labels.length === 0 && (
            <Typography variant="body2" color="text.secondary" align="center" sx={{ py: 1 }}>
              {t('tracker:workspaceLabel.empty')}
            </Typography>
          )}
        </Box>

        {/* 底部操作栏：一律左对齐 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderTop: 1, borderColor: 'divider' }}>
          <Button color="inherit" onClick={onClose} disabled={submitting || deleting}>
            {t('tracker:workspaceLabel.close')}
          </Button>
        </Box>
      </Box>

      {/* 删除确认 */}
      <Dialog open={deleteTarget !== null} onClose={deleting ? undefined : () => setDeleteTarget(null)}>
        <DialogTitle>{t('tracker:workspaceLabel.delete')}</DialogTitle>
        <DialogContent>
          <Typography>{t('tracker:workspaceLabel.deleteConfirmMsg', { name: deleteTarget?.name ?? '' })}</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDeleteTarget(null)} disabled={deleting}>
            {t('tracker:workspaceLabel.cancel')}
          </Button>
          <Button color="error" variant="contained" onClick={handleDelete} disabled={deleting}>
            {t('tracker:workspaceLabel.delete')}
          </Button>
        </DialogActions>
      </Dialog>

      <Snackbar
        open={toastOpen}
        autoHideDuration={2000}
        onClose={() => setToastOpen(false)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={toast.severity} variant="filled">{toast.text}</Alert>
      </Snackbar>
    </Drawer>
  );
}

export default WorkspaceLabelManagerDrawer;
