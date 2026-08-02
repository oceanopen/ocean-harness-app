import type { Priority, ProjectIssueResponseData, ProjectStateModel, StateGroup, WorkspaceProjectModel } from '@src/services';
import type { Dispatch, SetStateAction } from 'react';
import {
  AddOutlined as AddOutlinedIcon,
  AssignmentOutlined as AssignmentOutlinedIcon,
  CalendarMonthOutlined as CalendarMonthOutlinedIcon,
  ExpandLessOutlined as ExpandLessOutlinedIcon,
  ExpandMoreOutlined as ExpandMoreOutlinedIcon,
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
  IconButton,
  InputLabel,
  MenuItem,
  Select,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  Typography,
  useTheme,
} from '@mui/material';
import { formatDate } from '@src/shared/time';
import { useToast } from '@src/shared/useToast';
import { trackerKeys, useProjectIssues, useProjectStates } from '@src/state/tracker';
import { PriorityIcon } from '@src/windows/panel/TrackerPage/components/PriorityIcon';
import ProjectIssueDrawer from '@src/windows/panel/TrackerPage/components/ProjectIssueDrawer/ProjectIssueDrawer';
import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import KanbanView from './KanbanView/KanbanView';

// Issue 视图模式：列表（按状态组纵向分组）/ 看板（按具体状态横向分列 + 拖拽）。
type IssueViewMode = 'list' | 'kanban';

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

// 列表行内最多展示的标签数，超出显示 +N（对齐 plane list 的标签胶囊上限）。
const MAX_ROW_LABELS = 3;

interface IssueListPageProps {
  workspaceProject: WorkspaceProjectModel;
}

// Issue 列表页（嵌于 tracker 三栏壳的右栏）。
// projectIssue/state 列表走 useProjectIssues/useProjectStates（与详情抽屉/看板共享缓存）；增删改走 mutation（在抽屉内），
// 本页回调仅弹 toast（mutation 内部 invalidate）。看板拖拽乐观更新经 updateProjectIssues 适配器写回 Query 缓存。
function ProjectIssueListPage({ workspaceProject }: IssueListPageProps) {
  const { t } = useTranslation();
  const theme = useTheme();
  const qc = useQueryClient();
  const { data: projectIssues = [], isLoading: issuesLoading, isError: issuesError } = useProjectIssues(workspaceProject.id);
  const { data: projectStates = [], isLoading: statesLoading, isError: statesError } = useProjectStates(workspaceProject.id);
  const { show: showToast, snack } = useToast();
  const [keyword, setKeyword] = useState('');
  const [priorityFilter, setPriorityFilter] = useState<Priority | 'all'>('all');
  const [stateFilter, setStateFilter] = useState<number | 'all'>('all');
  const [collapsed, setCollapsed] = useState<Set<StateGroup>>(() => new Set());
  const [createOpen, setCreateOpen] = useState(false);
  // 创建抽屉预选状态：分组头"+"快捷新建时传入该组首个状态，工具栏/空态新建为 undefined。
  const [createInitialStateId, setCreateInitialStateId] = useState<number | undefined>(undefined);
  const [detailProjectIssue, setDetailProjectIssue] = useState<ProjectIssueResponseData | null>(null);
  // 视图模式按项目持久化（localStorage），默认列表。
  const [viewMode, setViewMode] = useState<IssueViewMode>(
    () => (localStorage.getItem(`tracker.viewMode.${workspaceProject.id}`) === 'kanban' ? 'kanban' : 'list'),
  );

  const handleViewModeChange = useCallback((mode: IssueViewMode) => {
    setViewMode(mode);
    localStorage.setItem(`tracker.viewMode.${workspaceProject.id}`, mode);
  }, [workspaceProject.id]);

  // 看板拖拽乐观更新适配器：把 useKanbanDnd 的 Dispatch<SetStateAction> 调用转写回 Query 缓存。
  // useKanbanDnd 的乐观更新/回滚/二次校正逻辑完全不变，仅底层从本地 state 切到 Query 缓存。
  const updateProjectIssues = useCallback<Dispatch<SetStateAction<ProjectIssueResponseData[]>>>((action) => {
    qc.setQueryData<ProjectIssueResponseData[]>(trackerKeys.projectIssues(workspaceProject.id), (prev) => {
      const base = prev ?? []; // prev 理论必为数组（useProjectIssues 默认 []），兜底仅为类型安全
      return typeof action === 'function' ? action(base) : action;
    });
  }, [qc, workspaceProject.id]);

  const stateMap = useMemo(() => {
    const m = new Map<number, ProjectStateModel>();
    projectStates.forEach(s => m.set(s.id, s));
    return m;
  }, [projectStates]);

  // 各状态组的首个状态 id（sortOrder 升序），供分组头"+"快捷新建预选该组状态。
  const firstStateIdByGroup = useMemo(() => {
    const m: Partial<Record<StateGroup, number>> = {};
    for (const sg of GROUP_ORDER) {
      const sorted = projectStates
        .filter(s => s.stateGroup === sg)
        .sort((a, b) => a.sortOrder - b.sortOrder);
      const first = sorted[0];
      if (first) {
        m[sg] = first.id;
      }
    }
    return m;
  }, [projectStates]);

  // 客户端筛选 + 按 stateGroup 分组 + 组内排序。
  const grouped = useMemo(() => {
    const q = keyword.trim().toLowerCase();
    const filtered = projectIssues.filter((i) => {
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
      // stateId 未知（如状态被删）fallback 到 backlog，保证 projectIssue 不丢。
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
  }, [projectIssues, keyword, priorityFilter, stateFilter, stateMap]);

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

  // 创建/更新/删除成功：mutation 内部已 invalidate，回调仅弹 toast + 关闭抽屉。
  const handleCreated = useCallback((projectIssue: ProjectIssueResponseData) => {
    showToast(t('tracker:projectIssue.toast.created', { name: projectIssue.name }), 'success');
  }, [t, showToast]);

  const handleUpdated = useCallback(() => {
    setDetailProjectIssue(null);
    showToast(t('tracker:projectIssue.toast.updated'), 'success');
  }, [showToast, t]);

  const handleDeleted = useCallback((issueId: number) => {
    setDetailProjectIssue(prev => (prev?.id === issueId ? null : prev));
    showToast(t('tracker:projectIssue.toast.deleted'), 'success');
  }, [showToast]);

  // 打开/关闭创建抽屉：openCreate 可预选状态（分组头"+"传该组首个状态；其余入口不预选）。
  const openCreate = useCallback((stateId?: number) => {
    setCreateInitialStateId(stateId);
    setCreateOpen(true);
  }, []);
  const closeCreate = useCallback(() => {
    setCreateOpen(false);
    setCreateInitialStateId(undefined);
  }, []);

  const isLoading = issuesLoading || statesLoading;
  const isError = issuesError || statesError;
  const ready = !isLoading && !isError;

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 工具栏：搜索 + 优先级筛选 + 状态筛选 + 新建 */}
      <Box
        sx={{
          p: 1.5,
          borderBottom: 1,
          borderColor: 'divider',
          bgcolor: 'action.hover',
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          flexWrap: 'wrap',
        }}
      >
        <TextField
          size="small"
          placeholder={t('tracker:projectIssue.search')}
          value={keyword}
          onChange={e => setKeyword(e.target.value)}
          sx={{ flexGrow: 1, minWidth: 120 }}
        />
        <FormControl size="small" sx={{ minWidth: 110 }}>
          <InputLabel>{t('tracker:projectIssue.filter.priority')}</InputLabel>
          <Select
            label={t('tracker:projectIssue.filter.priority')}
            value={priorityFilter}
            onChange={e => setPriorityFilter(e.target.value as Priority | 'all')}
          >
            <MenuItem value="all">{t('tracker:projectIssue.filter.all')}</MenuItem>
            {PRIORITY_ORDER.map(p => (
              <MenuItem key={p} value={p}>{t(`tracker:projectIssue.priority.${p}`)}</MenuItem>
            ))}
          </Select>
        </FormControl>
        <FormControl size="small" sx={{ minWidth: 120 }}>
          <InputLabel>{t('tracker:projectIssue.filter.state')}</InputLabel>
          <Select
            label={t('tracker:projectIssue.filter.state')}
            value={stateFilter}
            onChange={e => setStateFilter(e.target.value as number | 'all')}
          >
            <MenuItem value="all">{t('tracker:projectIssue.filter.allStates')}</MenuItem>
            {projectStates.map(s => (
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
          <ToggleButton value="list" aria-label={t('tracker:projectIssue.view.list')}>
            <ViewListOutlinedIcon />
          </ToggleButton>
          <ToggleButton value="kanban" aria-label={t('tracker:projectIssue.view.board')}>
            <ViewKanbanOutlinedIcon />
          </ToggleButton>
        </ToggleButtonGroup>
        <Button variant="contained" size="small" startIcon={<AddOutlinedIcon />} onClick={() => openCreate()}>
          {t('tracker:projectIssue.actions.add')}
        </Button>
      </Box>

      {/* 内容区 */}
      <Box sx={{ flex: 1, overflow: 'auto' }}>
        {isLoading && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <CircularProgress />
          </Box>
        )}
        {isError && (
          <Box sx={{ p: 2 }}>
            <Alert
              severity="error"
              action={(
                <Button
                  color="inherit"
                  size="small"
                  onClick={() => {
                    void qc.invalidateQueries({ queryKey: trackerKeys.projectIssues(workspaceProject.id) });
                    void qc.invalidateQueries({ queryKey: trackerKeys.projectStates(workspaceProject.id) });
                  }}
                >
                  {t('tracker:projectIssue.error.retry')}
                </Button>
              )}
            >
              <AlertTitle>{t('tracker:projectIssue.error.title')}</AlertTitle>
              {t('tracker:projectIssue.error.desc')}
            </Alert>
          </Box>
        )}
        {ready && projectIssues.length === 0 && (
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
              {t('tracker:projectIssue.empty.title')}
            </Typography>
            <Typography variant="body2" color="text.secondary" align="center">
              {t('tracker:projectIssue.empty.desc')}
            </Typography>
            <Button variant="contained" startIcon={<AddOutlinedIcon />} onClick={() => openCreate()} sx={{ mt: 1 }}>
              {t('tracker:projectIssue.actions.add')}
            </Button>
          </Box>
        )}
        {ready && projectIssues.length > 0 && viewMode === 'kanban' && (
          <KanbanView
            projectIssues={projectIssues}
            projectStates={projectStates}
            stateMap={stateMap}
            setIssues={updateProjectIssues}
            onOpen={setDetailProjectIssue}
            showToast={showToast}
          />
        )}
        {ready && projectIssues.length > 0 && viewMode === 'list' && totalCount === 0 && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <Typography variant="body2" color="text.secondary">
              {t('tracker:projectIssue.empty.noMatch')}
            </Typography>
          </Box>
        )}
        {ready && viewMode === 'list' && totalCount > 0 && (
          <Box sx={{ pb: 1 }}>
            {GROUP_ORDER.map((g) => {
              const arr = grouped[g];
              if (arr.length === 0) {
                return null;
              }
              const isCollapsed = collapsed.has(g);
              return (
                <Box key={g} sx={{ mb: 1 }}>
                  <Box
                    onClick={() => toggleGroup(g)}
                    sx={{
                      position: 'sticky',
                      top: 0,
                      zIndex: 1,
                      display: 'flex',
                      alignItems: 'center',
                      gap: 0.5,
                      px: 1.5,
                      py: 0.75,
                      borderBottom: 1,
                      borderColor: 'divider',
                      // 不透明灰带：sticky 吸顶需遮住下方滚动行，故用 grey token（action.hover 为半透明会透底）。
                      bgcolor: theme.palette.mode === 'light' ? 'grey.100' : 'grey.900',
                      cursor: 'pointer',
                    }}
                  >
                    {isCollapsed
                      ? <ExpandMoreOutlinedIcon fontSize="small" color="action" />
                      : <ExpandLessOutlinedIcon fontSize="small" color="action" />}
                    <Typography variant="caption" sx={{ fontWeight: 700 }}>
                      {t(`tracker:projectIssue.group.${g}`)}
                    </Typography>
                    <Chip label={arr.length} size="small" sx={{ height: 18, fontSize: '0.7rem' }} />
                    <Box sx={{ flex: 1 }} />
                    <Tooltip title={t('tracker:projectIssue.actions.add')}>
                      <IconButton
                        size="small"
                        aria-label={t('tracker:projectIssue.actions.add')}
                        onClick={(e) => {
                          e.stopPropagation();
                          openCreate(firstStateIdByGroup[g]);
                        }}
                      >
                        <AddOutlinedIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  </Box>
                  <Collapse in={!isCollapsed}>
                    {arr.map(projectIssue => (
                      <ProjectIssueRow
                        key={projectIssue.id}
                        projectIssue={projectIssue}
                        stateMap={stateMap}
                        onOpen={setDetailProjectIssue}
                      />
                    ))}
                  </Collapse>
                </Box>
              );
            })}
          </Box>
        )}
      </Box>

      {snack}

      {createOpen && (
        <ProjectIssueDrawer
          mode="create"
          workspaceProject={workspaceProject}
          projectStates={projectStates}
          initialStateId={createInitialStateId}
          onClose={closeCreate}
          onCreated={handleCreated}
        />
      )}

      {detailProjectIssue && (
        <ProjectIssueDrawer
          mode="edit"
          projectIssue={detailProjectIssue}
          workspaceProject={workspaceProject}
          projectStates={projectStates}
          onClose={() => setDetailProjectIssue(null)}
          onUpdated={handleUpdated}
          onDeleted={handleDeleted}
        />
      )}
    </Box>
  );
}

// 单行：状态色块（取 state.color，未知用 text.disabled）+ #id + 名称 + 优先级图标。
// 整行可点击打开侧滑详情（onOpen）。
interface IssueRowProps {
  projectIssue: ProjectIssueResponseData;
  stateMap: Map<number, ProjectStateModel>;
  onOpen: (projectIssue: ProjectIssueResponseData) => void;
}

function ProjectIssueRow({ projectIssue, stateMap, onOpen }: IssueRowProps) {
  const state = stateMap.get(projectIssue.stateId);
  // 当前列表打开时刻冻结的"现在"，用于逾期判断（不在 render 中调 new Date() 以保纯；解析字符串的 new Date(str) 是纯函数）。
  const [now] = useState(() => Date.now());
  const overdue = !!projectIssue.targetDate
    && new Date(projectIssue.targetDate).getTime() < now
    && state?.stateGroup !== 'completed'
    && state?.stateGroup !== 'cancelled';
  const labels = projectIssue.labels;

  return (
    <Box
      onClick={() => onOpen(projectIssue)}
      sx={[
        {
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          px: 1.5,
          py: 1,
          minHeight: 40,
          borderBottom: 1,
          borderColor: 'divider',
          cursor: 'pointer',
        },
        { '&:hover': { bgcolor: 'action.hover' } },
        // 组内最后一行去底线，避免与下一分组头/容器底形成双线。
        { '&:last-child': { borderBottomColor: 'transparent' } },
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
      <Typography variant="caption" color="text.disabled" sx={{ flexShrink: 0 }}>#{projectIssue.id}</Typography>
      <Typography variant="body2" sx={{ flex: 1, minWidth: 0, ...truncateSx }} title={projectIssue.name}>
        {projectIssue.name}
      </Typography>
      {labels.length > 0 && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexShrink: 0 }}>
          {labels.slice(0, MAX_ROW_LABELS).map(l => (
            <Tooltip key={l.id} title={l.name}>
              <Box
                sx={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 0.5,
                  maxWidth: 120,
                  px: 0.75,
                  py: 0.25,
                  borderRadius: 0.5,
                  border: 1,
                  borderColor: l.color || 'divider',
                }}
              >
                <Box sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: l.color, flexShrink: 0 }} />
                <Typography variant="caption" sx={{ ...truncateSx }}>{l.name}</Typography>
              </Box>
            </Tooltip>
          ))}
          {labels.length > MAX_ROW_LABELS && (
            <Typography variant="caption" color="text.disabled">+{labels.length - MAX_ROW_LABELS}</Typography>
          )}
        </Box>
      )}
      {projectIssue.targetDate && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexShrink: 0, color: overdue ? 'error.main' : 'text.disabled' }}>
          <CalendarMonthOutlinedIcon sx={{ fontSize: '0.9rem' }} />
          <Typography variant="caption" color="inherit">{formatDate(projectIssue.targetDate, 'YYYY-MM-DD')}</Typography>
        </Box>
      )}
      <PriorityIcon priority={projectIssue.priority} />
    </Box>
  );
}

export default ProjectIssueListPage;
