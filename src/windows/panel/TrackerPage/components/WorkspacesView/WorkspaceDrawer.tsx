import type { WorkspaceModel } from '@src/services';
import { CloseOutlined as CloseOutlinedIcon } from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  Drawer,
  IconButton,
  TextField,
  Typography,
} from '@mui/material';
import { useCreateWorkspace, useUpdateWorkspace } from '@src/state/tracker';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

// 新建/编辑工作空间抽屉。
// 传入 workspace 时为编辑模式：标题改为"编辑工作空间"、ID 只读展示、字段反显、提交调用 update；
// 不传则为新建模式，行为不变。
//
// 由父组件按需挂载（{open && <WorkspaceDrawer/>}）：每次打开都是全新 useState 初值，
// 无需重置 effect；关闭即卸载。
//
// slug 派生（仅新建模式且用户未手动改过 slug）：name 变化 → slug = slugify(name)。
// 用户手动编辑 slug 后置 slugTouched=true，停止派生；编辑模式初始即视为 touched（尊重既有 slug）。
interface WorkspaceDrawerProps {
  onClose: () => void;
  onCreated: (ws: WorkspaceModel) => void;
  onUpdated?: (ws: WorkspaceModel) => void;
  workspace?: WorkspaceModel;
}

// 描述最大字数（与后端 binding max=500 对齐）。
const DESCRIPTION_MAX = 500;

// slug 规范化：小写 + 非 [a-z0-9] 序列折叠为单连字符 + 去首尾连字符。
// 中文等非 ASCII 字符会被剔除，结果可能为空串（由 canSubmit 拦下，提示用户手填）。
function slugify(input: string): string {
  return input
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

function WorkspaceDrawer({ onClose, onCreated, onUpdated, workspace }: WorkspaceDrawerProps) {
  const { t } = useTranslation();
  const isEdit = !!workspace;
  const createWs = useCreateWorkspace();
  const updateWs = useUpdateWorkspace();
  const [name, setName] = useState(workspace?.name ?? '');
  const [slug, setSlug] = useState(workspace?.slug ?? '');
  const [description, setDescription] = useState(workspace?.description ?? '');
  // 编辑模式初始即视为已触碰：改 name 不应覆盖既有 slug。
  const [slugTouched, setSlugTouched] = useState(isEdit);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleNameChange = (value: string) => {
    setName(value);
    if (!slugTouched) {
      setSlug(slugify(value));
    }
    setError(null);
  };

  const handleSlugChange = (value: string) => {
    setSlug(value);
    setSlugTouched(true);
    setError(null);
  };

  const canSubmit = name.trim().length > 0 && slug.trim().length > 0 && !submitting;

  const handleConfirm = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const payload = {
        name: name.trim(),
        slug: slug.trim(),
        description: description.trim(),
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
      setError(isEdit ? t('tracker:toast.updateFailed', { message: msg }) : t('tracker:toast.createFailed', { message: msg }));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Drawer
      anchor="right"
      open
      // 提交中禁止背景点击/Esc 关闭，避免半成品状态丢失。
      onClose={submitting ? undefined : onClose}
      sx={{ '& .MuiDrawer-paper': { width: { xs: '100%', sm: '50%' } } }}
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
            onChange={e => handleNameChange(e.target.value)}
            fullWidth
            autoFocus
            disabled={submitting}
          />
          <TextField
            label={t('tracker:workspace.add.slug')}
            placeholder={t('tracker:workspace.add.slugPlaceholder')}
            value={slug}
            onChange={e => handleSlugChange(e.target.value)}
            fullWidth
            disabled={submitting}
            helperText={t('tracker:workspace.add.slugHint')}
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
    </Drawer>
  );
}

export default WorkspaceDrawer;
