import type { WorkspaceTypeModel } from '@src/services';
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
  IconButton,
  TextField,
  Typography,
} from '@mui/material';
import ResizableDrawer from '@src/shared/ResizableDrawer';
import { useToast } from '@src/shared/useToast';
import { useCreateWorkspaceType, useDeleteWorkspaceType, useUpdateWorkspaceType, useWorkspaceTypes } from '@src/state/tracker';
import { useState } from 'react';
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

// workspace 类型管理抽屉（CRUD）：顶部新建/编辑表单（name + 色板/hex + description）+ 已有类型列表。
// 类型列表走 TanStack Query 共享缓存（issue 抽屉同源），mutation 内部 invalidate，无需 onChanged 桥接。
// create 不传 sortOrder（后端 MAX+10000）；update 直接覆盖 color/description；
// delete 将引用该类型的 issue 置为未分类。
// 列表为空时展示「需求」「缺陷」两条默认建议项（含预设色），点「添加」才真正落库生效。
interface TypeManagerDrawerProps {
  workspaceId: number;
  onClose: () => void;
}

// 默认建议类型（仅空列表时展示，点添加才落库）：需求=蓝、缺陷=红。
const DEFAULT_TYPE_PRESETS = [
  { nameKey: 'tracker:workspaceType.defaultRequirement', color: '#3b82f6' },
  { nameKey: 'tracker:workspaceType.defaultDefect', color: '#ef4444' },
] as const;

function WorkspaceTypeManagerDrawer({ workspaceId, onClose }: TypeManagerDrawerProps) {
  const { t } = useTranslation();
  const { show: showToast, snack } = useToast();
  const typesQuery = useWorkspaceTypes(workspaceId);
  const types = typesQuery.data ?? [];
  const createType = useCreateWorkspaceType(workspaceId);
  const updateType = useUpdateWorkspaceType(workspaceId);
  const deleteType = useDeleteWorkspaceType(workspaceId);
  const [editId, setEditId] = useState<number | null>(null);
  const [name, setName] = useState('');
  const [color, setColor] = useState(COLOR_PRESETS[0]);
  const [description, setDescription] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [creatingDefault, setCreatingDefault] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<WorkspaceTypeModel | null>(null);
  const [deleting, setDeleting] = useState(false);

  const isEdit = editId !== null;
  const canSubmit = name.trim().length > 0 && !submitting;

  const resetForm = () => {
    setEditId(null);
    setName('');
    setColor(COLOR_PRESETS[0]);
    setDescription('');
    setError(null);
  };

  const startEdit = (ty: WorkspaceTypeModel) => {
    setEditId(ty.id);
    setName(ty.name);
    setColor(ty.color || COLOR_PRESETS[0]);
    setDescription(ty.description);
    setError(null);
  };

  const handleCreateOrUpdate = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const payload = { name: name.trim(), color: color.trim(), description: description.trim() };
      if (isEdit && editId !== null) {
        await updateType.mutateAsync({ id: editId, ...payload });
        showToast(t('tracker:projectIssue.toast.typeUpdated'), 'success');
      } else {
        await createType.mutateAsync({ workspaceId, ...payload });
        showToast(t('tracker:projectIssue.toast.typeCreated', { name: payload.name }), 'success');
      }
      resetForm();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setError(t('tracker:projectIssue.toast.typeOpFailed', { message: msg }));
    } finally {
      setSubmitting(false);
    }
  };

  // 空列表默认建议项：点「添加」才落库（前端预填，提交生效）。
  const handleCreateDefault = async (preset: (typeof DEFAULT_TYPE_PRESETS)[number]) => {
    setCreatingDefault(preset.nameKey);
    try {
      await createType.mutateAsync({ workspaceId, name: t(preset.nameKey), color: preset.color });
      showToast(t('tracker:projectIssue.toast.typeCreated', { name: t(preset.nameKey) }), 'success');
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:projectIssue.toast.typeOpFailed', { message: msg }), 'error');
    } finally {
      setCreatingDefault(null);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) {
      return;
    }
    setDeleting(true);
    try {
      await deleteType.mutateAsync(deleteTarget.id);
      showToast(t('tracker:projectIssue.toast.typeDeleted'), 'success');
      if (editId === deleteTarget.id) {
        resetForm();
      }
      setDeleteTarget(null);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:projectIssue.toast.typeOpFailed', { message: msg }), 'error');
    } finally {
      setDeleting(false);
    }
  };

  return (
    <ResizableDrawer
      open
      // 提交/删除中禁止背景点击/Esc 关闭，避免半成品状态丢失。
      onClose={submitting || deleting ? undefined : onClose}
      defaultWidthPct={40}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* 头部 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }} noWrap>
            {t('tracker:workspaceType.title')}
          </Typography>
          <IconButton size="small" onClick={onClose} disabled={submitting || deleting} aria-label={t('tracker:workspaceType.close')}>
            <CloseOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 内容 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: 2, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
          {/* 新建/编辑表单 */}
          <TextField
            label={t('tracker:workspaceType.name')}
            placeholder={t('tracker:workspaceType.namePlaceholder')}
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
            <Typography variant="caption" color="text.secondary">{t('tracker:workspaceType.color')}</Typography>
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
            label={t('tracker:workspaceType.description')}
            placeholder={t('tracker:workspaceType.descriptionPlaceholder')}
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
              {isEdit ? t('tracker:workspaceType.save') : t('tracker:workspaceType.create')}
            </Button>
            {isEdit && (
              <Button color="inherit" size="small" onClick={resetForm} disabled={submitting}>
                {t('tracker:workspaceType.cancel')}
              </Button>
            )}
          </Box>

          {types.length > 0 && <Divider sx={{ my: 0.5 }} />}

          {/* 已有类型列表 */}
          {types.map(ty => (
            <Box key={ty.id} sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <Box sx={{ width: 12, height: 12, borderRadius: '50%', bgcolor: ty.color || 'text.disabled', flexShrink: 0 }} />
              <Typography variant="body2" sx={{ flex: 1, minWidth: 0 }} noWrap>{ty.name}</Typography>
              {ty.description && <Chip label={ty.description} size="small" variant="outlined" sx={{ maxWidth: 160 }} />}
              <IconButton size="small" onClick={() => startEdit(ty)} aria-label={t('tracker:workspaceType.edit')}>
                <EditOutlinedIcon />
              </IconButton>
              <IconButton size="small" onClick={() => setDeleteTarget(ty)} aria-label={t('tracker:workspaceType.delete')}>
                <DeleteOutlinedIcon />
              </IconButton>
            </Box>
          ))}
          {types.length === 0 && !typesQuery.isLoading && (
            <>
              <Typography variant="body2" color="text.secondary" align="center" sx={{ py: 1 }}>
                {t('tracker:workspaceType.empty')}
              </Typography>
              {/* 默认建议类型（需求/缺陷）：点「添加」才落库生效 */}
              {DEFAULT_TYPE_PRESETS.map(preset => (
                <Box key={preset.nameKey} sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <Box sx={{ width: 12, height: 12, borderRadius: '50%', bgcolor: preset.color, flexShrink: 0 }} />
                  <Typography variant="body2" sx={{ flex: 1, minWidth: 0 }} noWrap>{t(preset.nameKey)}</Typography>
                  <Button
                    size="small"
                    startIcon={<AddOutlinedIcon />}
                    onClick={() => void handleCreateDefault(preset)}
                    disabled={creatingDefault !== null}
                  >
                    {creatingDefault === preset.nameKey
                      ? t('tracker:workspaceType.adding')
                      : t('tracker:workspaceType.add')}
                  </Button>
                </Box>
              ))}
            </>
          )}
        </Box>

        {/* 底部操作栏：一律左对齐 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderTop: 1, borderColor: 'divider' }}>
          <Button color="inherit" onClick={onClose} disabled={submitting || deleting}>
            {t('tracker:workspaceType.close')}
          </Button>
        </Box>
      </Box>

      {/* 删除确认 */}
      <Dialog open={deleteTarget !== null} onClose={deleting ? undefined : () => setDeleteTarget(null)}>
        <DialogTitle>{t('tracker:workspaceType.delete')}</DialogTitle>
        <DialogContent>
          <Typography>{t('tracker:workspaceType.deleteConfirmMsg', { name: deleteTarget?.name ?? '' })}</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDeleteTarget(null)} disabled={deleting}>
            {t('tracker:workspaceType.cancel')}
          </Button>
          <Button color="error" variant="contained" onClick={handleDelete} disabled={deleting}>
            {t('tracker:workspaceType.delete')}
          </Button>
        </DialogActions>
      </Dialog>

      {snack}
    </ResizableDrawer>
  );
}

export default WorkspaceTypeManagerDrawer;
