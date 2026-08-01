import type { Repository, RepoSubDir } from '@src/shared/bindings';
import {
  AddOutlined as AddOutlinedIcon,
  FolderOpen as FolderOpenIcon,
  RemoveCircleOutlined as RemoveCircleOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  IconButton,
  InputAdornment,
  TextField,
  Typography,
} from '@mui/material';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { basename, relativeSubDir } from '@src/shared/repoPath';
import { open as openDialog } from '@tauri-apps/plugin-dialog';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

// 添加/编辑本地仓库弹窗。
// 传入 repo 时为编辑模式：标题改为"编辑仓库"、ID 只读展示、name/dir/description/subDirList 默认反显、
// 提交调用 updateRepository；不传则为添加模式，行为不变。
//
// 由父组件按需挂载（{open && <AddRepositoryDialog/>}）：每次打开都是全新 useState 初值，
// 无需重置 effect；关闭即卸载。
//
// 名称派生（仅新增模式）：选择/变更仓库目录 → name = basename(dir)。项目子目录不影响仓库名称
// （一个仓库可对应多个子目录，子目录不应改写仓库整体名称）。
interface AddRepositoryDialogProps {
  onClose: () => void;
  onAdded: (repo: Repository) => void;
  onUpdated?: (repo: Repository) => void;
  repo?: Repository;
}

// 描述最大字数（仓库描述与子目录描述共用，与后端 cap_description 对齐）。
const DESCRIPTION_MAX = 200;

// 表单内子目录行：在持久化的 RepoSubDir 基础上追加客户端唯一 _key，用作 React list key（避免 index key 警告）。
// _key 不入库，提交前剥离。
type SubDirRow = RepoSubDir & { _key: string };

function AddRepositoryDialog({ onClose, onAdded, onUpdated, repo }: AddRepositoryDialogProps) {
  const { t } = useTranslation();
  const isEdit = !!repo;
  const [name, setName] = useState(repo?.name ?? '');
  const [dir, setDir] = useState(repo?.dir ?? '');
  const [description, setDescription] = useState(repo?.description ?? '');
  // 行 key 顺序生成器（同一时刻仅一个弹窗实例，顺序 id 即可保证唯一）。
  const keySeqRef = useRef(0);
  const nextKey = () => `sub-${keySeqRef.current++}`;
  const [subDirs, setSubDirs] = useState<SubDirRow[]>(() =>
    (repo?.subDirList ?? []).map(s => ({ ...s, _key: nextKey() })),
  );
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 新增模式下据仓库目录派生仓库名称；编辑模式不动（保留用户自定义名称）。
  const deriveName = (newDir: string) => {
    if (isEdit) {
      return;
    }
    setName(basename(newDir));
  };

  const handleBrowse = async () => {
    // directory: true 多选关闭，返回 string | null。
    const selected = await openDialog({ directory: true, multiple: false });
    if (typeof selected === 'string') {
      setDir(selected);
      setError(null);
      deriveName(selected);
    }
  };

  // 某一行项目子目录的文件夹选择：剥离仓库目录前缀得相对路径；不在仓库目录下则报错。
  const handleBrowseSubDir = async (idx: number) => {
    const root = dir.trim();
    if (!root) {
      setError(t('repositories:toast.invalidSubDir'));
      return;
    }
    const selected = await openDialog({ directory: true, multiple: false });
    if (typeof selected === 'string') {
      const rel = relativeSubDir(root, selected);
      if (rel === null) {
        setError(t('repositories:toast.invalidSubDir'));
        return;
      }
      setSubDirs(prev => prev.map((s, i) => (i === idx ? { ...s, subDir: rel } : s)));
      setError(null);
    }
  };

  const addSubDir = () => {
    setSubDirs(prev => [...prev, { subDir: '', subDirDescription: '', _key: nextKey() }]);
  };

  const removeSubDir = (idx: number) => {
    setSubDirs(prev => prev.filter((_, i) => i !== idx));
  };

  const updateSubDirField = (idx: number, field: keyof RepoSubDir, value: string) => {
    setSubDirs(prev => prev.map((s, i) => (i === idx ? { ...s, [field]: value } : s)));
    setError(null);
  };

  const canSubmit = name.trim().length > 0 && dir.trim().length > 0 && !submitting;

  const handleConfirm = async () => {
    setSubmitting(true);
    setError(null);
    try {
      // 过滤 subDir 为空的行（未填写路径的行无意义，不入库；后端也会再过滤一次），并剥离客户端 _key。
      const cleanedSubDirs = subDirs
        .filter(s => s.subDir.trim().length > 0)
        .map(({ _key: _omit, ...rest }) => rest);
      if (isEdit && repo) {
        const updated = await unwrap(
          commands.updateRepository(repo.id, name.trim(), dir.trim(), description.trim(), cleanedSubDirs),
        );
        onUpdated?.(updated);
      } else {
        const added = await unwrap(
          commands.addRepository(name.trim(), dir.trim(), description.trim(), cleanedSubDirs),
        );
        onAdded(added);
      }
      onClose();
    } catch (e) {
      // 后端哨兵字符串 → i18n 文案；未知错误带原始信息。
      const err = String(e);
      if (err === 'not-a-git-repo') {
        setError(t('repositories:toast.notGitRepo'));
      } else if (err === 'dir-exists') {
        setError(t('repositories:toast.dirExists'));
      } else if (err === 'invalid-sub-dir') {
        setError(t('repositories:toast.invalidSubDir'));
      } else if (isEdit) {
        setError(t('repositories:toast.updateFailed', { message: err }));
      } else {
        setError(t('repositories:toast.addFailed', { message: err }));
      }
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
      <DialogTitle>{isEdit ? t('repositories:edit.title') : t('repositories:add.title')}</DialogTitle>
      <DialogContent>
        <Box sx={{ mt: 0.5, display: 'flex', flexDirection: 'column', gap: 2 }}>
          {isEdit && (
            <TextField
              label={t('repositories:edit.idLabel')}
              value={repo!.id}
              fullWidth
              slotProps={{ input: { readOnly: true } }}
              variant="filled"
            />
          )}
          <TextField
            label={t('repositories:add.name')}
            placeholder={t('repositories:add.namePlaceholder')}
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
            label={t('repositories:add.dir')}
            placeholder={t('repositories:add.dirPlaceholder')}
            value={dir}
            onChange={(e) => {
              setDir(e.target.value);
              setError(null);
            }}
            fullWidth
            disabled={submitting}
            slotProps={{
              input: {
                endAdornment: (
                  <InputAdornment position="end">
                    <IconButton
                      edge="end"
                      onClick={handleBrowse}
                      disabled={submitting}
                      aria-label={t('repositories:add.browse')}
                    >
                      <FolderOpenIcon />
                    </IconButton>
                  </InputAdornment>
                ),
              },
            }}
          />
          <TextField
            label={t('repositories:add.description')}
            placeholder={t('repositories:add.descriptionPlaceholder')}
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

          {/* 项目子目录列表：默认空，点击「添加项目子目录」动态生成行，可生成多项。 */}
          <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 1 }}>
              <Typography variant="caption" sx={{ color: 'text.disabled' }}>
                {t('repositories:add.subDirSection')}
              </Typography>
              <Button
                size="small"
                variant="outlined"
                startIcon={<AddOutlinedIcon />}
                onClick={addSubDir}
                disabled={submitting}
              >
                {t('repositories:add.addSubDir')}
              </Button>
            </Box>
            {subDirs.length === 0 && (
              <Divider />
            )}
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, mt: subDirs.length > 0 ? 1 : 0 }}>
              {subDirs.map((s, idx) => (
                <Box
                  key={s._key}
                  sx={{
                    border: 1,
                    borderColor: 'divider',
                    borderRadius: 1,
                    p: 1.5,
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 1,
                  }}
                >
                  <TextField
                    label={t('repositories:add.subDirSection')}
                    placeholder={t('repositories:add.subDirPlaceholder')}
                    value={s.subDir}
                    onChange={e => updateSubDirField(idx, 'subDir', e.target.value)}
                    fullWidth
                    size="small"
                    disabled={submitting}
                    slotProps={{
                      input: {
                        endAdornment: (
                          <InputAdornment position="end">
                            <IconButton
                              edge="end"
                              size="small"
                              onClick={() => handleBrowseSubDir(idx)}
                              disabled={submitting}
                              aria-label={t('repositories:add.browse')}
                            >
                              <FolderOpenIcon fontSize="small" />
                            </IconButton>
                          </InputAdornment>
                        ),
                      },
                    }}
                  />
                  <Box sx={{ display: 'flex', gap: 1, alignItems: 'flex-start' }}>
                    <TextField
                      placeholder={t('repositories:add.subDirDescPlaceholder')}
                      value={s.subDirDescription}
                      onChange={e => updateSubDirField(idx, 'subDirDescription', e.target.value)}
                      fullWidth
                      size="small"
                      disabled={submitting}
                      slotProps={{ htmlInput: { maxLength: DESCRIPTION_MAX } }}
                    />
                    <IconButton
                      size="small"
                      color="error"
                      onClick={() => removeSubDir(idx)}
                      disabled={submitting}
                      aria-label={t('repositories:add.removeSubDir')}
                    >
                      <RemoveCircleOutlinedIcon fontSize="small" />
                    </IconButton>
                  </Box>
                </Box>
              ))}
            </Box>
          </Box>

          {error && <Alert severity="error">{error}</Alert>}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button color="inherit" onClick={onClose} disabled={submitting}>
          {t('repositories:add.cancel')}
        </Button>
        <Button variant="contained" onClick={handleConfirm} disabled={!canSubmit}>
          {isEdit ? t('repositories:edit.confirm') : t('repositories:add.confirm')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

export default AddRepositoryDialog;
