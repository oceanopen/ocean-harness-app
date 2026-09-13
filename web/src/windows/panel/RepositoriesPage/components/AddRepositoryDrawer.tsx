import type { LocalRepositoryModel, RepoSubDir } from '@src/services';
import {
  AddOutlined as AddOutlinedIcon,
  CloseOutlined as CloseOutlinedIcon,
  FolderOpen as FolderOpenIcon,
  RemoveCircleOutlined as RemoveCircleOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  Divider,
  IconButton,
  InputAdornment,
  TextField,
  Typography,
} from '@mui/material';
import { basename, relativeSubDir } from '@src/shared/repoPath';
import ResizableDrawer from '@src/shared/ResizableDrawer';
import { useCreateLocalRepository, useUpdateLocalRepository } from '@src/state/localRepositories';
import { open as openDialog } from '@tauri-apps/plugin-dialog';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

// 添加/编辑本地仓库抽屉。
interface AddRepositoryDrawerProps {
  onClose: () => void;
  repo?: LocalRepositoryModel;
}

// 描述最大字数（仓库描述与子目录描述共用，与后端 capDescription 对齐）。
const DESCRIPTION_MAX = 200;

// 表单内子目录行：在持久化的 RepoSubDir 基础上追加客户端唯一 _key，用作 React list key（避免 index key 警告）。
// _key 不入库，提交前剥离。
type SubDirRow = RepoSubDir & { _key: string };

// 从未知错误对象提取 message（Go 经 http.ts 抛 new Error(msg)，Rust IPC 抛裸字符串，两者兼容）。
function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

function AddRepositoryDrawer({ onClose, repo }: AddRepositoryDrawerProps) {
  const { t } = useTranslation();
  const isEdit = !!repo;
  const createMu = useCreateLocalRepository();
  const updateMu = useUpdateLocalRepository();
  const submitting = isEdit ? updateMu.isPending : createMu.isPending;

  const [name, setName] = useState(repo?.name ?? '');
  const [localDir, setLocalDir] = useState(repo?.localDir ?? '');
  const [description, setDescription] = useState(repo?.description ?? '');
  // 行 key 顺序生成器（同一时刻仅一个抽屉实例，顺序 id 即可保证唯一）。
  const keySeqRef = useRef(0);
  const nextKey = () => `sub-${keySeqRef.current++}`;
  const [subDirs, setSubDirs] = useState<SubDirRow[]>(() =>
    (repo?.subDirList ?? []).map(s => ({ ...s, _key: nextKey() })),
  );
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
      setLocalDir(selected);
      setError(null);
      deriveName(selected);
    }
  };

  // 某一行项目子目录的文件夹选择：剥离仓库目录前缀得相对路径；不在仓库目录下则报错。
  const handleBrowseSubDir = async (idx: number) => {
    const root = localDir.trim();
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

  const canSubmit = name.trim().length > 0 && localDir.trim().length > 0 && !submitting;

  const handleConfirm = async () => {
    setError(null);
    try {
      // 过滤 subDir 为空的行（未填写路径的行无意义，不入库；后端也会再过滤一次），并剥离客户端 _key。
      const cleanedSubDirs = subDirs
        .filter(s => s.subDir.trim().length > 0)
        .map(({ _key: _omit, ...rest }) => rest);
      const payload = {
        name: name.trim(),
        localDir: localDir.trim(),
        description: description.trim(),
        subDirList: cleanedSubDirs,
      };
      if (isEdit && repo) {
        await updateMu.mutateAsync({ id: repo.id, ...payload });
      } else {
        await createMu.mutateAsync(payload);
      }
      onClose();
    } catch (e) {
      // 统一展示后台返回的错误信息（后端返回可读中文），不做细分映射。
      setError(errMsg(e));
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
            {isEdit ? t('repositories:edit.title') : t('repositories:add.title')}
          </Typography>
          <IconButton size="small" onClick={onClose} disabled={submitting} aria-label={t('repositories:add.cancel')}>
            <CloseOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 内容 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: 2, display: 'flex', flexDirection: 'column', gap: 2 }}>
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
            value={localDir}
            onChange={(e) => {
              setLocalDir(e.target.value);
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

        {/* 底部操作栏：一律左对齐 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderTop: 1, borderColor: 'divider' }}>
          <Button color="inherit" onClick={onClose} disabled={submitting}>
            {t('repositories:add.cancel')}
          </Button>
          <Button variant="contained" onClick={handleConfirm} disabled={!canSubmit}>
            {isEdit ? t('repositories:edit.confirm') : t('repositories:add.confirm')}
          </Button>
        </Box>
      </Box>
    </ResizableDrawer>
  );
}

export default AddRepositoryDrawer;
