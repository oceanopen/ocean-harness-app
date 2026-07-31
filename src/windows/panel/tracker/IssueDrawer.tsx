import type { Issue, Priority, ProjectState, WorkspaceLabel } from './IssueListPage';
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
import RichTextEditor from './components/RichTextEditor';
import StateSelect from './components/StateSelect';
import 'dayjs/locale/zh-cn';

type ToastSeverity = 'success' | 'error';

interface IssueDrawerProps {
  mode: 'create' | 'edit';
  project: Project;
  states: ProjectState[];
  issue?: Issue; // edit 模式必传
  onClose: () => void;
  onCreated?: (issue: Issue) => void;
  onUpdated?: (issue: Issue) => void;
  onDeleted?: (issueId: number) => void;
}

// Issue 抽屉（create/edit 共用）。所有字段（含 labels）本地态，点保存/创建一次性提交，
// 成功后关闭抽屉 + 父级刷新列表；失败 drawer 内弹 error toast（成功 toast 由父级统一弹）。
// mode 仅决定初值来源、提交 API、头部标题/元信息/删除按钮显隐。
function IssueDrawer({ mode, project, states, issue, onClose, onCreated, onUpdated, onDeleted }: IssueDrawerProps) {
  const { t, i18n } = useTranslation();
  const isZh = i18n.language?.toLowerCase().startsWith('zh') ?? false;
  // 属性字段本地态（挂载即按 mode/issue 初始化，每次打开新挂载，无需 reset effect）。
  const defaultStateId = states.find(s => s.isDefault === 'Y')?.id ?? states[0]?.id ?? 0;
  const [name, setName] = useState(issue?.name ?? '');
  const [description, setDescription] = useState(issue?.description ?? '');
  const [stateId, setStateId] = useState(issue?.stateId ?? defaultStateId);
  const [priority, setPriority] = useState<Priority>(issue?.priority ?? 'none');
  const [startDate, setStartDate] = useState(issue?.startDate ?? '');
  const [targetDate, setTargetDate] = useState(issue?.targetDate ?? '');
  const [labels, setLabels] = useState<WorkspaceLabel[]>(issue?.labels ?? []);
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
      // 标签管理里若有删除，剔除本地已选 labels 中已不存在的。
      setLabels(prev => prev.filter(l => data.some(w => w.id === l.id)));
    } catch {
      // 标签选项加载失败不阻塞编辑。
    }
  }, [project.workspaceId]);

  useEffect(() => {
    void loadWsLabels();
  }, [loadWsLabels]);

  // labels 是否相对原值变化（id 集合比较，覆盖增/删/替换）。
  const labelsDirty = (() => {
    const cur = new Set(labels.map(l => l.id));
    const orig = new Set((issue?.labels ?? []).map(l => l.id));
    if (cur.size !== orig.size) {
      return true;
    }
    for (const id of cur) {
      if (!orig.has(id)) {
        return true;
      }
    }
    return false;
  })();

  const dirty = mode === 'edit' && !!issue && (
    name !== issue.name
    || description !== issue.description
    || stateId !== issue.stateId
    || priority !== issue.priority
    || startDate !== issue.startDate
    || targetDate !== issue.targetDate
    || labelsDirty
  );

  const canSubmit = mode === 'create' ? name.trim().length > 0 : !!dirty;

  // labels 本地切换（不发请求）：create/edit 统一，提交时随 create/update 一起发。
  const handleToggleLabel = useCallback((labelId: number) => {
    setLabels((prev) => {
      if (prev.some(l => l.id === labelId)) {
        return prev.filter(l => l.id !== labelId);
      }
      const found = wsLabels.find(w => w.id === labelId);
      return found ? [...prev, found] : prev;
    });
  }, [wsLabels]);

  const handleSave = async () => {
    setSubmitting(true);
    try {
      if (mode === 'create') {
        const created = await apiPost<Issue>('/api/tracker/projectIssue/create', {
          projectId: project.id,
          workspaceId: project.workspaceId,
          name: name.trim(),
          description,
          priority,
          startDate,
          targetDate,
          stateId,
          labelIds: labels.map(l => l.id),
        });
        onCreated?.(created);
        onClose();
      } else {
        const updated = await apiPost<Issue>('/api/tracker/projectIssue/update', {
          id: issue!.id,
          name: name.trim(),
          description,
          stateId,
          priority,
          startDate,
          targetDate,
          labelIds: labels.map(l => l.id),
        });
        onUpdated?.(updated);
        onClose();
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(
        mode === 'create'
          ? t('tracker:issue.toast.createFailed', { message: msg })
          : t('tracker:issue.toast.updateFailed', { message: msg }),
        'error',
      );
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!issue) {
      return;
    }
    setDeleting(true);
    try {
      await apiPost('/api/tracker/projectIssue/delete', { id: issue.id });
      onDeleted?.(issue.id);
      onClose();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:issue.toast.deleteFailed', { message: msg }), 'error');
    } finally {
      setDeleting(false);
      setDeleteOpen(false);
    }
  };

  return (
    <Drawer anchor="right" open onClose={onClose} sx={{ '& .MuiDrawer-paper': { width: { xs: '100%', sm: 480 } } }}>
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* 头部 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }} noWrap>
            {mode === 'edit' && issue ? `#${issue.id} ${issue.name}` : t('tracker:issue.create.title')}
          </Typography>
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
            <RichTextEditor
              value={description}
              onChange={setDescription}
              placeholder={t('tracker:issue.rte.placeholder')}
              disabled={submitting || deleting}
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
          {/* 元信息（仅 edit） */}
          {mode === 'edit' && issue && (
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
          )}
        </Box>

        {/* 底部操作栏 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderTop: 1, borderColor: 'divider' }}>
          {mode === 'edit' && (
            <Button
              color="error"
              size="small"
              startIcon={<DeleteOutlinedIcon />}
              onClick={() => setDeleteOpen(true)}
              disabled={submitting || deleting}
            >
              {t('tracker:issue.detail.delete')}
            </Button>
          )}
          <Box sx={{ flex: 1 }} />
          <Button color="inherit" onClick={onClose} disabled={submitting || deleting}>
            {t('tracker:issue.create.cancel')}
          </Button>
          <Button variant="contained" onClick={handleSave} disabled={!canSubmit || submitting}>
            {mode === 'create' ? t('tracker:issue.create.confirm') : t('tracker:issue.detail.save')}
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

      {mode === 'edit' && (
        <Dialog open={deleteOpen} onClose={deleting ? undefined : () => setDeleteOpen(false)}>
          <DialogTitle>{t('tracker:issue.detail.deleteTitle')}</DialogTitle>
          <DialogContent>
            <Typography>{t('tracker:issue.detail.deleteConfirmMsg', { name: issue?.name ?? '' })}</Typography>
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
      )}

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

export default IssueDrawer;
