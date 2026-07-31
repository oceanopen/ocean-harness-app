import type { Issue, ProjectState, WorkspaceLabel } from './IssueListPage';
import type { Project } from './ProjectListPage';
import { CloseOutlined as CloseOutlinedIcon, DeleteOutlined as DeleteOutlinedIcon } from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Drawer,
  IconButton,
  Snackbar,
  TextField,
  Typography,
} from '@mui/material';
import { DatePicker, LocalizationProvider } from '@mui/x-date-pickers';
import { AdapterDayjs } from '@mui/x-date-pickers/AdapterDayjs';
import { formatDate } from '@src/shared/time';
import dayjs from 'dayjs';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { apiPost } from './api';
import LabelManagerDialog from './components/LabelManagerDialog';
import LabelSelect from './components/LabelSelect';
import PrioritySelect from './components/PrioritySelect';
import StateSelect from './components/StateSelect';
import 'dayjs/locale/zh-cn';

type ToastSeverity = 'success' | 'error';

// 属性字段（保存按钮统一提交）；标签走 toggleIssue 即时生效（onLabelsChanged 静默同步父级，不弹 toast）。
interface IssueDetailDrawerProps {
  issue: Issue;
  project: Project;
  states: ProjectState[];
  onClose: () => void;
  onUpdated: (issue: Issue) => void;
  onDeleted: (issueId: number) => void;
  onLabelsChanged: (issueId: number, labels: WorkspaceLabel[]) => void;
}

function IssueDetailDrawer({ issue, project, states, onClose, onUpdated, onDeleted, onLabelsChanged }: IssueDetailDrawerProps) {
  const { t, i18n } = useTranslation();
  // 日历面板随当前语言中文化（dayjs locale + x-date-pickers 文案包）。
  const isZh = i18n.language?.toLowerCase().startsWith('zh') ?? false;
  // 属性字段本地态（挂载即新建，无需 reset effect）。
  const [name, setName] = useState(issue.name);
  const [description, setDescription] = useState(issue.description);
  const [stateId, setStateId] = useState(issue.stateId);
  const [priority, setPriority] = useState(issue.priority);
  const [startDate, setStartDate] = useState(issue.startDate);
  const [targetDate, setTargetDate] = useState(issue.targetDate);
  const [labels, setLabels] = useState<WorkspaceLabel[]>(issue.labels);
  const [wsLabels, setWsLabels] = useState<WorkspaceLabel[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [managerOpen, setManagerOpen] = useState(false);
  const [toast, setToast] = useState<{ text: string; severity: ToastSeverity }>({ text: '', severity: 'success' });
  const [toastOpen, setToastOpen] = useState(false);

  const showToast = useCallback((text: string, severity: ToastSeverity) => {
    setToast({ text, severity });
    setToastOpen(true);
  }, []);

  const loadWsLabels = useCallback(async () => {
    try {
      const data = await apiPost<WorkspaceLabel[]>('/api/tracker/workspaceLabel/getList', { workspaceId: project.workspaceId });
      setWsLabels(data);
      // 标签管理里若有删除，剔除本地 issue labels 中已不存在的。
      setLabels(prev => prev.filter(l => data.some(w => w.id === l.id)));
    } catch {
      // 标签选项加载失败不阻塞详情编辑。
    }
  }, [project.workspaceId]);

  useEffect(() => {
    void loadWsLabels();
  }, [loadWsLabels]);

  const dirty = name !== issue.name
    || description !== issue.description
    || stateId !== issue.stateId
    || priority !== issue.priority
    || startDate !== issue.startDate
    || targetDate !== issue.targetDate;

  const handleSave = async () => {
    setSubmitting(true);
    try {
      // 一次提交全部属性字段；不发 isDraft（保留原值）。后端按 stateId 自动维护 completedAt。
      const updated = await apiPost<Issue>('/api/tracker/projectIssue/update', {
        id: issue.id,
        name: name.trim(),
        description,
        stateId,
        priority,
        startDate,
        targetDate,
      });
      // 用返回值同步本地态（消除 dirty + 同步 labels/completedAt）。
      setName(updated.name);
      setDescription(updated.description);
      setStateId(updated.stateId);
      setPriority(updated.priority);
      setStartDate(updated.startDate);
      setTargetDate(updated.targetDate);
      setLabels(updated.labels);
      onUpdated(updated);
      showToast(t('tracker:issue.toast.updated'), 'success');
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:issue.toast.updateFailed', { message: msg }), 'error');
    } finally {
      setSubmitting(false);
    }
  };

  const handleToggleLabel = async (labelId: number) => {
    try {
      const returned = await apiPost<WorkspaceLabel[]>('/api/tracker/workspaceLabel/toggleIssue', {
        issueId: issue.id,
        labelId,
      });
      setLabels(returned);
      onLabelsChanged(issue.id, returned);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:issue.toast.labelOpFailed', { message: msg }), 'error');
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await apiPost('/api/tracker/projectIssue/delete', { id: issue.id });
      showToast(t('tracker:issue.toast.deleted'), 'success');
      onDeleted(issue.id);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:issue.toast.deleteFailed', { message: msg }), 'error');
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Drawer anchor="right" open onClose={onClose} sx={{ '& .MuiDrawer-paper': { width: { xs: '100%', sm: 420 } } }}>
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* 头部 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }} noWrap>#{issue.id} {issue.name}</Typography>
          <IconButton size="small" onClick={onClose} aria-label={t('tracker:issue.detail.close')}>
            <CloseOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 内容 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: 2, display: 'flex', flexDirection: 'column', gap: 2 }}>
          <TextField
            label={t('tracker:issue.detail.name')}
            value={name}
            onChange={e => setName(e.target.value)}
            fullWidth
            size="small"
            disabled={submitting || deleting}
            slotProps={{ htmlInput: { maxLength: 255 } }}
          />
          <Box>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
              {t('tracker:issue.detail.description')}
            </Typography>
            <TextField
              value={description}
              onChange={e => setDescription(e.target.value)}
              fullWidth
              size="small"
              multiline
              minRows={3}
              maxRows={8}
              disabled={submitting || deleting}
              placeholder={t('tracker:issue.detail.descriptionPlaceholder')}
            />
          </Box>
          <LocalizationProvider dateAdapter={AdapterDayjs} adapterLocale={isZh ? 'zh-cn' : 'en'}>
            <Box sx={{ display: 'grid', gridTemplateColumns: '80px 1fr', gap: 1.5, alignItems: 'center' }}>
              <Typography variant="body2" color="text.secondary">{t('tracker:issue.detail.state')}</Typography>
              <StateSelect value={stateId} states={states} onChange={setStateId} disabled={submitting || deleting} />
              <Typography variant="body2" color="text.secondary">{t('tracker:issue.detail.priority')}</Typography>
              <PrioritySelect value={priority} onChange={setPriority} disabled={submitting || deleting} />
              <Typography variant="body2" color="text.secondary">{t('tracker:issue.detail.startDate')}</Typography>
              <DatePicker
                format="YYYY-MM-DD"
                value={startDate ? dayjs(startDate) : null}
                onChange={v => setStartDate(v ? v.format('YYYY-MM-DD') : '')}
                disabled={submitting || deleting}
                slotProps={{ textField: { size: 'small', fullWidth: true } }}
              />
              <Typography variant="body2" color="text.secondary">{t('tracker:issue.detail.targetDate')}</Typography>
              <DatePicker
                format="YYYY-MM-DD"
                value={targetDate ? dayjs(targetDate) : null}
                onChange={v => setTargetDate(v ? v.format('YYYY-MM-DD') : '')}
                disabled={submitting || deleting}
                slotProps={{ textField: { size: 'small', fullWidth: true } }}
              />
            </Box>
          </LocalizationProvider>
          <LabelSelect
            issueLabels={labels}
            options={wsLabels}
            onToggle={handleToggleLabel}
            onOpenManager={() => setManagerOpen(true)}
            disabled={submitting || deleting}
          />
          {/* 元信息 */}
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 2, mt: 1 }}>
            <Typography variant="caption" color="text.disabled">
              {t('tracker:issue.detail.createdAt')} {formatDate(issue.createdAt, 'YYYY-MM-DD HH:mm')}
            </Typography>
            {issue.completedAt && (
              <Typography variant="caption" color="text.disabled">
                {t('tracker:issue.detail.completedAt')} {formatDate(issue.completedAt, 'YYYY-MM-DD HH:mm')}
              </Typography>
            )}
          </Box>
        </Box>

        {/* 底部操作栏 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderTop: 1, borderColor: 'divider' }}>
          <Button
            color="error"
            size="small"
            startIcon={<DeleteOutlinedIcon />}
            onClick={() => setDeleteOpen(true)}
            disabled={submitting || deleting}
          >
            {t('tracker:issue.detail.delete')}
          </Button>
          <Box sx={{ flex: 1 }} />
          <Button variant="contained" onClick={handleSave} disabled={!dirty || submitting}>
            {t('tracker:issue.detail.save')}
          </Button>
        </Box>
      </Box>

      {managerOpen && (
        <LabelManagerDialog
          workspaceId={project.workspaceId}
          labels={wsLabels}
          onClose={() => setManagerOpen(false)}
          onChanged={loadWsLabels}
        />
      )}

      <Dialog open={deleteOpen} onClose={deleting ? undefined : () => setDeleteOpen(false)}>
        <DialogTitle>{t('tracker:issue.detail.deleteTitle')}</DialogTitle>
        <DialogContent>
          <Typography>{t('tracker:issue.detail.deleteConfirmMsg', { name: issue.name })}</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDeleteOpen(false)} disabled={deleting}>
            {t('tracker:issue.detail.cancel')}
          </Button>
          <Button color="error" variant="contained" onClick={handleDelete} disabled={deleting}>
            {t('tracker:issue.detail.delete')}
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

export default IssueDetailDrawer;
