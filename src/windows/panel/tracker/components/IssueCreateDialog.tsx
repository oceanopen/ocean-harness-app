import type { Issue, Priority } from '../IssueListPage';
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  TextField,
} from '@mui/material';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { apiPost } from '../api';

// 快速新建 Issue 弹窗（仅 name + priority，极简；描述/日期/标签留给任务12 侧滑详情）。
// 由父组件按需挂载（{open && <IssueCreateDialog/>}）：每次打开都是全新 useState 初值，
// 无需重置 effect；关闭即卸载。
// stateId/sortOrder 不传——后端取 project.default_state_id、sortOrder 自算 MAX+10000。
interface IssueCreateDialogProps {
  projectId: number;
  workspaceId: number;
  onClose: () => void;
  onCreated: (issue: Issue) => void;
}

// 名称最大字数（与后端 binding max=255 对齐）。
const NAME_MAX = 255;

// 优先级下拉顺序（urgent 在前），值即 enums.Priority 合法值。
const PRIORITY_OPTIONS: Priority[] = ['urgent', 'high', 'medium', 'low', 'none'];

function IssueCreateDialog({ projectId, workspaceId, onClose, onCreated }: IssueCreateDialogProps) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [priority, setPriority] = useState<Priority>('none');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canSubmit = name.trim().length > 0 && !submitting;

  const handleConfirm = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const created = await apiPost<Issue>('/api/tracker/projectIssue/create', {
        projectId,
        workspaceId,
        name: name.trim(),
        priority,
      });
      onCreated(created);
      onClose();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setError(t('tracker:issue.toast.createFailed', { message: msg }));
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
      <DialogTitle>{t('tracker:issue.add.title')}</DialogTitle>
      <DialogContent>
        <Box sx={{ mt: 0.5, display: 'flex', flexDirection: 'column', gap: 2 }}>
          <TextField
            label={t('tracker:issue.add.name')}
            placeholder={t('tracker:issue.add.namePlaceholder')}
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              setError(null);
            }}
            fullWidth
            autoFocus
            disabled={submitting}
            slotProps={{ htmlInput: { maxLength: NAME_MAX } }}
          />
          <FormControl fullWidth size="small" disabled={submitting}>
            <InputLabel>{t('tracker:issue.add.priority')}</InputLabel>
            <Select
              label={t('tracker:issue.add.priority')}
              value={priority}
              onChange={e => setPriority(e.target.value as Priority)}
            >
              {PRIORITY_OPTIONS.map(p => (
                <MenuItem key={p} value={p}>
                  {t(`tracker:issue.priority.${p}`)}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
          {error && <Alert severity="error">{error}</Alert>}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button color="inherit" onClick={onClose} disabled={submitting}>
          {t('tracker:issue.add.cancel')}
        </Button>
        <Button variant="contained" onClick={handleConfirm} disabled={!canSubmit}>
          {t('tracker:issue.add.confirm')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

export default IssueCreateDialog;
