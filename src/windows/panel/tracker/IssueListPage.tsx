import type { Priority, ProjectIssueResponseData, ProjectStateModel, StateGroup, WorkspaceProjectModel } from '@src/service';
import {
  AddOutlined as AddOutlinedIcon,
  AssignmentOutlined as AssignmentOutlinedIcon,
  DragHandleRounded as DragHandleRoundedIcon,
  ExpandLessOutlined as ExpandLessOutlinedIcon,
  ExpandMoreOutlined as ExpandMoreOutlinedIcon,
  KeyboardArrowDownRounded as KeyboardArrowDownRoundedIcon,
  KeyboardArrowUpRounded as KeyboardArrowUpRoundedIcon,
  KeyboardDoubleArrowUpRounded as KeyboardDoubleArrowUpRoundedIcon,
  RemoveOutlined as RemoveOutlinedIcon,
  ViewKanbanOutlined as ViewKanbanOutlinedIcon,
  ViewListOutlined as ViewListOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Chip,
  CircularProgress,
  Collapse,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  Snackbar,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  Typography,
} from '@mui/material';
import { ProjectIssueService, ProjectStateService } from '@src/service';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import IssueDrawer from './IssueDrawer';
import KanbanView from './kanban/KanbanView';

type LoadStatus = 'loading' | 'ready' | 'error';

// Issue 视图模式：列表（按状态组纵向分组）/ 看板（按具体状态横向分列 + 拖拽）。
type IssueViewMode = 'list' | 'kanban';

// 浮层 toast 严重级别（成功用 success，失败用 error）。
type ToastSeverity = 'success' | 'error';

// Priority / StateGroup / ProjectIssueResponseData / ProjectStateModel / WorkspaceLabelModel 类型
// 已迁移至 @src/service（ProjectIssueService / ProjectStateService 等）。

// 优先级业务权重（升序，urgent 在前）——后端 orderBy=priority 为文本字典序不可靠，前端按 weight 重排。
const PRIORITY_WEIGHT: Record<Priority, number> = {
  urgent: 0,
  high: 1,
  medium: 2,
  low: 3,
  none: 4,
};

const PRIORITY_ORDER: Priority[] = ['urgent', 'high', 'medium', 'low', 'none'];

// 分组顺序固定（对齐 state_group 工作流语义）。
const GROUP_ORDER: StateGroup[] = ['backlog', 'unstarted', 'started', 'completed', 'cancelled'];

const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

interface IssueListPageProps {
  project: WorkspaceProjectModel;
}

// Issue 列表页（嵌于 tracker 三栏壳的右栏）。
// 并行拉 issue + state 两接口 → 构建 stateId→state 映射 → 按 stateGroup 分组（Collapse 可折叠）。
// 筛选走客户端（与 ProjectListPage 一致，已加载列表上过滤，不重复请求）：关键字 + 优先级 + 状态。
// 组内排序：priority weight 升序，同优先级按 sortOrder 升序。
function IssueListPage({ project }: IssueListPageProps) {
  const { t } = useTranslation();
  const [status, setStatus] = useState<LoadStatus>('loading');
  const [issues, setIssues] = useState<ProjectIssueResponseData[]>([]);
  const [states, setStates] = useState<ProjectStateModel[]>([]);
  const [toast, setToast] = useState<{ text: string; severity: ToastSeverity }>({ text: '', severity: 'success' });
  const [toastOpen, setToastOpen] = useState(false);
  const [keyword, setKeyword] = useState('');
  const [priorityFilter, setPriorityFilter] = useState<Priority | 'all'>('all');
  const [stateFilter, setStateFilter] = useState<number | 'all'>('all');
  const [collapsed, setCollapsed] = useState<Set<StateGroup>>(() => new Set());
  const [createOpen, setCreateOpen] = useState(false);
  const [detailIssue, setDetailIssue] = useState<ProjectIssueResponseData | null>(null);
  // 视图模式按项目持久化（localStorage），默认列表。
  const [viewMode, setViewMode] = useState<IssueViewMode>(
    () => (localStorage.getItem(`tracker.viewMode.${project.id}`) === 'kanban' ? 'kanban' : 'list'),
  );

  const handleViewModeChange = useCallback((mode: IssueViewMode) => {
    setViewMode(mode);
    localStorage.setItem(`tracker.viewMode.${project.id}`, mode);
  }, [project.id]);

  const showToast = useCallback((text: string, severity: ToastSeverity) => {
    setToast({ text, severity });
    setToastOpen(true);
  }, []);

  const load = useCallback(async () => {
    setStatus('loading');
    try {
      const [issueData, stateData] = await Promise.all([
        ProjectIssueService.getList({ projectId: project.id }),
        ProjectStateService.getList({ projectId: project.id }),
      ]);
      setIssues(issueData);
      setStates(stateData);
      setStatus('ready');
    } catch {
      setStatus('error');
    }
  }, [project.id]);

  // 挂载时加载一次（父级用 key={project.id} 保证切换项目重挂载，自然重载）。
  useEffect(() => {
    void load();
  }, [load]);

  const stateMap = useMemo(() => {
    const m = new Map<number, ProjectStateModel>();
    states.forEach(s => m.set(s.id, s));
    return m;
  }, [states]);

  // 客户端筛选 + 按 stateGroup 分组 + 组内排序。
  const grouped = useMemo(() => {
    const q = keyword.trim().toLowerCase();
    const filtered = issues.filter((i) => {
      if (q && !i.name.toLowerCase().includes(q)) {
        return false;
      }
      if (priorityFilter !== 'all' && i.priority !== priorityFilter) {
        return false;
      }
      if (stateFilter !== 'all' && i.stateId !== stateFilter) {
        return false;
      }
      return true;
    });
    const buckets: Record<StateGroup, ProjectIssueResponseData[]> = {
      backlog: [],
      unstarted: [],
      started: [],
      completed: [],
      cancelled: [],
    };
    filtered.forEach((i) => {
      // stateId 未知（如状态被删）fallback 到 backlog，保证 issue 不丢。
      const group = stateMap.get(i.stateId)?.stateGroup ?? 'backlog';
      buckets[group].push(i);
    });
    Object.values(buckets).forEach(arr => arr.sort((a, b) => {
      const dw = PRIORITY_WEIGHT[a.priority] - PRIORITY_WEIGHT[b.priority];
      if (dw !== 0) {
        return dw;
      }
      return a.sortOrder - b.sortOrder;
    }));
    return buckets;
  }, [issues, keyword, priorityFilter, stateFilter, stateMap]);

  const totalCount = useMemo(
    () => Object.values(grouped).reduce((sum, arr) => sum + arr.length, 0),
    [grouped],
  );

  const toggleGroup = useCallback((g: StateGroup) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(g)) {
        next.delete(g);
      } else {
        next.add(g);
      }
      return next;
    });
  }, []);

  const handleCreated = useCallback((issue: ProjectIssueResponseData) => {
    setIssues(prev => [...prev, issue]);
    showToast(t('tracker:issue.toast.created', { name: issue.name }), 'success');
  }, [t, showToast]);

  // 编辑保存：就地替换 issue + 关闭抽屉（统一模型：保存即关闭刷新）+ toast。
  const handleUpdated = useCallback((updated: ProjectIssueResponseData) => {
    setIssues(prev => prev.map(i => (i.id === updated.id ? updated : i)));
    setDetailIssue(null);
    showToast(t('tracker:issue.toast.updated'), 'success');
  }, [showToast]);

  // 删除 issue：从列表剔除 + 关闭 drawer + toast。
  const handleDeleted = useCallback((issueId: number) => {
    setIssues(prev => prev.filter(i => i.id !== issueId));
    setDetailIssue(prev => (prev?.id === issueId ? null : prev));
    showToast(t('tracker:issue.toast.deleted'), 'success');
  }, [showToast]);

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 工具栏：搜索 + 优先级筛选 + 状态筛选 + 新建 */}
      <Box
        sx={{
          p: 1.5,
          borderBottom: 1,
          borderColor: 'divider',
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          flexWrap: 'wrap',
        }}
      >
        <TextField
          size="small"
          placeholder={t('tracker:issue.search')}
          value={keyword}
          onChange={e => setKeyword(e.target.value)}
          sx={{ flexGrow: 1, minWidth: 120 }}
        />
        <FormControl size="small" sx={{ minWidth: 110 }}>
          <InputLabel>{t('tracker:issue.filter.priority')}</InputLabel>
          <Select
            label={t('tracker:issue.filter.priority')}
            value={priorityFilter}
            onChange={e => setPriorityFilter(e.target.value as Priority | 'all')}
          >
            <MenuItem value="all">{t('tracker:issue.filter.all')}</MenuItem>
            {PRIORITY_ORDER.map(p => (
              <MenuItem key={p} value={p}>{t(`tracker:issue.priority.${p}`)}</MenuItem>
            ))}
          </Select>
        </FormControl>
        <FormControl size="small" sx={{ minWidth: 120 }}>
          <InputLabel>{t('tracker:issue.filter.state')}</InputLabel>
          <Select
            label={t('tracker:issue.filter.state')}
            value={stateFilter}
            onChange={e => setStateFilter(e.target.value as number | 'all')}
          >
            <MenuItem value="all">{t('tracker:issue.filter.allStates')}</MenuItem>
            {states.map(s => (
              <MenuItem key={s.id} value={s.id}>{s.name}</MenuItem>
            ))}
          </Select>
        </FormControl>
        <ToggleButtonGroup
          size="small"
          exclusive
          value={viewMode}
          onChange={(_, v) => {
            if (v) {
              handleViewModeChange(v);
            }
          }}
        >
          <ToggleButton value="list" aria-label={t('tracker:issue.view.list')}>
            <ViewListOutlinedIcon />
          </ToggleButton>
          <ToggleButton value="kanban" aria-label={t('tracker:issue.view.board')}>
            <ViewKanbanOutlinedIcon />
          </ToggleButton>
        </ToggleButtonGroup>
        <Button variant="contained" size="small" startIcon={<AddOutlinedIcon />} onClick={() => setCreateOpen(true)}>
          {t('tracker:issue.actions.add')}
        </Button>
      </Box>

      {/* 内容区 */}
      <Box sx={{ flex: 1, overflow: 'auto' }}>
        {status === 'loading' && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <CircularProgress />
          </Box>
        )}
        {status === 'error' && (
          <Box sx={{ p: 2 }}>
            <Alert
              severity="error"
              action={(
                <Button color="inherit" size="small" onClick={load}>
                  {t('tracker:issue.error.retry')}
                </Button>
              )}
            >
              <AlertTitle>{t('tracker:issue.error.title')}</AlertTitle>
              {t('tracker:issue.error.desc')}
            </Alert>
          </Box>
        )}
        {status === 'ready' && issues.length === 0 && (
          <Box
            sx={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
              gap: 1.5,
              px: 3,
              py: 4,
            }}
          >
            <AssignmentOutlinedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              {t('tracker:issue.empty.title')}
            </Typography>
            <Typography variant="body2" color="text.secondary" align="center">
              {t('tracker:issue.empty.desc')}
            </Typography>
            <Button variant="contained" startIcon={<AddOutlinedIcon />} onClick={() => setCreateOpen(true)} sx={{ mt: 1 }}>
              {t('tracker:issue.actions.add')}
            </Button>
          </Box>
        )}
        {status === 'ready' && issues.length > 0 && viewMode === 'kanban' && (
          <KanbanView
            issues={issues}
            states={states}
            stateMap={stateMap}
            setIssues={setIssues}
            onOpen={setDetailIssue}
            showToast={showToast}
          />
        )}
        {status === 'ready' && issues.length > 0 && viewMode === 'list' && totalCount === 0 && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <Typography variant="body2" color="text.secondary">
              {t('tracker:issue.empty.noMatch')}
            </Typography>
          </Box>
        )}
        {status === 'ready' && viewMode === 'list' && totalCount > 0 && (
          <Box sx={{ p: 1 }}>
            {GROUP_ORDER.map((g) => {
              const arr = grouped[g];
              if (arr.length === 0) {
                return null;
              }
              const isCollapsed = collapsed.has(g);
              return (
                <Box key={g} sx={{ mb: 0.5 }}>
                  <Box
                    onClick={() => toggleGroup(g)}
                    sx={[
                      {
                        display: 'flex',
                        alignItems: 'center',
                        gap: 0.5,
                        px: 1,
                        py: 0.75,
                        borderRadius: 1,
                        cursor: 'pointer',
                      },
                      { '&:hover': { bgcolor: 'action.hover' } },
                    ]}
                  >
                    {isCollapsed
                      ? <ExpandMoreOutlinedIcon fontSize="small" color="action" />
                      : <ExpandLessOutlinedIcon fontSize="small" color="action" />}
                    <Typography variant="caption" sx={{ fontWeight: 600 }}>
                      {t(`tracker:issue.group.${g}`)}
                    </Typography>
                    <Chip label={arr.length} size="small" sx={{ height: 18, fontSize: '0.7rem' }} />
                  </Box>
                  <Collapse in={!isCollapsed}>
                    {arr.map(issue => (
                      <IssueRow
                        key={issue.id}
                        issue={issue}
                        stateMap={stateMap}
                        onOpen={setDetailIssue}
                      />
                    ))}
                  </Collapse>
                </Box>
              );
            })}
          </Box>
        )}
      </Box>

      <Snackbar
        open={toastOpen}
        autoHideDuration={2000}
        onClose={() => setToastOpen(false)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={toast.severity} variant="filled">
          {toast.text}
        </Alert>
      </Snackbar>

      {createOpen && (
        <IssueDrawer
          mode="create"
          project={project}
          states={states}
          onClose={() => setCreateOpen(false)}
          onCreated={handleCreated}
        />
      )}

      {detailIssue && (
        <IssueDrawer
          mode="edit"
          issue={detailIssue}
          project={project}
          states={states}
          onClose={() => setDetailIssue(null)}
          onUpdated={handleUpdated}
          onDeleted={handleDeleted}
        />
      )}
    </Box>
  );
}

// 优先级图标：urgent 双上箭头(红) / high 上箭头(橙) / medium 横杠(蓝) / low 下箭头(灰) / none 减号(浅灰)。
export function PriorityIcon({ priority }: { priority: Priority }) {
  switch (priority) {
    case 'urgent':
      return <KeyboardDoubleArrowUpRoundedIcon sx={{ fontSize: '1rem', color: 'error.main' }} />;
    case 'high':
      return <KeyboardArrowUpRoundedIcon sx={{ fontSize: '1rem', color: 'warning.main' }} />;
    case 'medium':
      return <DragHandleRoundedIcon sx={{ fontSize: '1rem', color: 'info.main' }} />;
    case 'low':
      return <KeyboardArrowDownRoundedIcon sx={{ fontSize: '1rem', color: 'text.secondary' }} />;
    default:
      return <RemoveOutlinedIcon sx={{ fontSize: '1rem', color: 'text.disabled' }} />;
  }
}

// 单行：状态色块（取 state.color，未知用 text.disabled）+ #id + 名称 + 优先级图标。
// 整行可点击打开侧滑详情（onOpen）。
interface IssueRowProps {
  issue: ProjectIssueResponseData;
  stateMap: Map<number, ProjectStateModel>;
  onOpen: (issue: ProjectIssueResponseData) => void;
}

function IssueRow({ issue, stateMap, onOpen }: IssueRowProps) {
  const state = stateMap.get(issue.stateId);

  return (
    <Box
      onClick={() => onOpen(issue)}
      sx={[
        {
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          mx: 0.5,
          px: 1,
          py: 0.75,
          borderRadius: 1,
          cursor: 'pointer',
        },
        { '&:hover': { bgcolor: 'action.hover' } },
      ]}
    >
      <Tooltip title={state?.name ?? ''}>
        <Box
          sx={{
            width: 10,
            height: 10,
            borderRadius: '50%',
            bgcolor: state?.color || 'text.disabled',
            flexShrink: 0,
          }}
        />
      </Tooltip>
      <Typography variant="caption" color="text.disabled" sx={{ flexShrink: 0 }}>#{issue.id}</Typography>
      <Typography variant="body2" sx={{ flex: 1, minWidth: 0, ...truncateSx }} title={issue.name}>
        {issue.name}
      </Typography>
      <PriorityIcon priority={issue.priority} />
    </Box>
  );
}

export default IssueListPage;
