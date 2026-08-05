import type { Priority, ProjectIssueResponseData, ProjectStateModel, StateGroup, WorkspaceProjectModel } from '@src/services';
import type { Dispatch, SetStateAction } from 'react';
import {
  AddOutlined as AddOutlinedIcon,
  AssignmentOutlined as AssignmentOutlinedIcon,
  Autorenew as AutorenewIcon,
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
  CircularProgress,
  Collapse,
  IconButton,
  Paper,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import { useToast } from '@src/shared/useToast';
import { trackerKeys, useProjectIssues, useProjectStates } from '@src/state/tracker';
import { PRIORITY_WEIGHT } from '@src/windows/panel/TrackerPage/components/priorityMeta';
import PrioritySelect from '@src/windows/panel/TrackerPage/components/ProjectIssueDrawer/PrioritySelect';
import ProjectIssueDrawer from '@src/windows/panel/TrackerPage/components/ProjectIssueDrawer/ProjectIssueDrawer';
import ProjectStateSelect from '@src/windows/panel/TrackerPage/components/ProjectIssueDrawer/ProjectStateSelect';
import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import IssueCard from './IssueCard';
import KanbanView from './KanbanView/KanbanView';
import StateGroupCard from './StateGroupCard';

// Issue 视图模式：列表（按状态组纵向分组）/ 看板（按具体状态横向分列 + 拖拽）。
type IssueViewMode = 'list' | 'kanban';

// 分组顺序固定（对齐 state_group 工作流语义）。
const GROUP_ORDER: StateGroup[] = ['backlog', 'unstarted', 'started', 'completed', 'cancelled'];

interface IssueListProps {
  workspaceProject: WorkspaceProjectModel;
}

// Issue 列表（嵌于 tracker 三栏壳的右栏）。
// projectIssue/state 列表走 useProjectIssues/useProjectStates（与抽屉/看板/卡片共享缓存）；增删改走 mutation（在抽屉内），
// 本页回调仅弹 toast（mutation 内部 invalidate）。看板拖拽乐观更新经 updateProjectIssues 适配器写回 Query 缓存。
function ProjectIssueList({ workspaceProject }: IssueListProps) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: projectIssues = [], isLoading: issuesLoading, isError: issuesError, isFetching: issuesFetching } = useProjectIssues(workspaceProject.id);
  const { data: projectStates = [], isLoading: statesLoading, isError: statesError, isFetching: statesFetching } = useProjectStates(workspaceProject.id);
  const { show: showToast, snack } = useToast();
  const [keyword, setKeyword] = useState('');
  const [priorityFilter, setPriorityFilter] = useState<Priority | 'all'>('all');
  const [stateFilter, setStateFilter] = useState<number | 'all'>('all');
  const [collapsed, setCollapsed] = useState<Set<StateGroup>>(() => new Set());
  const [createOpen, setCreateOpen] = useState(false);
  // 创建抽屉预选状态：分组头"+"快捷新建时传入该组首个状态，工具栏/空态新建为 undefined。
  const [createInitialStateId, setCreateInitialStateId] = useState<number | undefined>(undefined);
  // 新建子 issue 的父 issue（非空即打开"新建子 issue"抽屉）。
  const [createChildParent, setCreateChildParent] = useState<ProjectIssueResponseData | null>(null);
  // 编辑抽屉：无子级卡片点击或编辑 icon 进入。
  const [editIssue, setEditIssue] = useState<ProjectIssueResponseData | null>(null);
  // 展开的父 issue 集合（列表/看板共享，内联展开子 issue 卡片）。
  const [expandedParents, setExpandedParents] = useState<Set<number>>(() => new Set());
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

  // 各父 issue 的子任务统计（done/total），用于卡片进度小标（从全量扁平 issue 派生）。
  const subtaskStats = useMemo(() => {
    const m = new Map<number, { done: number; total: number }>();
    for (const i of projectIssues) {
      if (i.parentId <= 0) {
        continue;
      }
      const s = m.get(i.parentId) ?? { done: 0, total: 0 };
      s.total += 1;
      if (i.completedAt) {
        s.done += 1;
      }
      m.set(i.parentId, s);
    }
    return m;
  }, [projectIssues]);

  // 各父 issue 的子 issue 列表（按 sortOrder 升序），用于卡片内联展开渲染。
  const childrenByParent = useMemo(() => {
    const m = new Map<number, ProjectIssueResponseData[]>();
    for (const i of projectIssues) {
      if (i.parentId <= 0) {
        continue;
      }
      const arr = m.get(i.parentId);
      if (arr) {
        arr.push(i);
      } else {
        m.set(i.parentId, [i]);
      }
    }
    m.forEach(arr => arr.sort((a, b) => a.sortOrder - b.sortOrder));
    return m;
  }, [projectIssues]);

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
      if (i.parentId !== 0) {
        return false; // 子任务不进主列表（随父卡片内联展开）
      }
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

  const toggleExpand = useCallback((id: number) => {
    setExpandedParents((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  // 创建/更新/删除成功：mutation 内部已 invalidate，回调仅弹 toast + 关闭抽屉。
  const handleCreated = useCallback((projectIssue: ProjectIssueResponseData) => {
    showToast(t('tracker:projectIssue.toast.created', { name: projectIssue.name }), 'success');
  }, [t, showToast]);

  // 新建子 issue 成功：弹 toast + 自动展开父级，便于看到新建的子卡片。
  const handleChildCreated = useCallback((projectIssue: ProjectIssueResponseData) => {
    showToast(t('tracker:projectIssue.toast.created', { name: projectIssue.name }), 'success');
    setExpandedParents((prev) => {
      if (!createChildParent || prev.has(createChildParent.id)) {
        return prev;
      }
      return new Set(prev).add(createChildParent.id);
    });
  }, [t, showToast, createChildParent]);

  const handleUpdated = useCallback(() => {
    setEditIssue(null);
    showToast(t('tracker:projectIssue.toast.updated'), 'success');
  }, [showToast, t]);

  const handleDeleted = useCallback((issueId: number) => {
    setEditIssue(prev => (prev?.id === issueId ? null : prev));
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

  const openCreateChild = useCallback((parent: ProjectIssueResponseData) => {
    setCreateChildParent(parent);
  }, []);

  const isLoading = issuesLoading || statesLoading;
  const isError = issuesError || statesError;
  // 后台刷新中（驱动刷新按钮的禁用与旋转）：两个 query 任一在拉取即为刷新中。
  const refreshing = issuesFetching || statesFetching;
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
        {/* 查询表单项：仅列表视图展示（看板按状态分列展示全量，筛选无意义，隐藏避免歧义） */}
        {viewMode === 'list' && (
          <>
            <TextField
              size="small"
              placeholder={t('tracker:projectIssue.search')}
              value={keyword}
              onChange={e => setKeyword(e.target.value)}
              sx={{ flexGrow: 1, minWidth: 120 }}
            />
            <PrioritySelect
              value={priorityFilter}
              onChange={setPriorityFilter}
              label={t('tracker:projectIssue.filter.priority')}
              allOption={t('tracker:projectIssue.filter.all')}
              sx={{ minWidth: 110 }}
            />
            <ProjectStateSelect
              value={stateFilter}
              onChange={setStateFilter}
              projectStates={projectStates}
              label={t('tracker:projectIssue.filter.state')}
              allOption={t('tracker:projectIssue.filter.allStates')}
              sx={{ minWidth: 120 }}
            />
          </>
        )}
        {/* 看板模式下用弹性间隔把"新建"推到右侧（列表模式搜索框 flexGrow 已撑开） */}
        {viewMode === 'kanban' && <Box sx={{ flex: 1 }} />}
        <Button variant="contained" startIcon={<AddOutlinedIcon />} onClick={() => openCreate()}>
          {t('tracker:projectIssue.actions.add')}
        </Button>
        <IconButton
          size="small"
          onClick={() => {
            void qc.invalidateQueries({ queryKey: trackerKeys.projectIssues(workspaceProject.id) });
          }}
          disabled={refreshing}
          aria-label={t('tracker:projectIssue.actions.refresh')}
        >
          <AutorenewIcon
            sx={{
              'animation': refreshing ? 'spin 0.8s linear infinite' : undefined,
              '@keyframes spin': {
                from: { transform: 'rotate(0deg)' },
                to: { transform: 'rotate(360deg)' },
              },
            }}
          />
        </IconButton>
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
            subtaskStats={subtaskStats}
            childrenByParent={childrenByParent}
            expandedParents={expandedParents}
            setIssues={updateProjectIssues}
            onAddIssue={openCreate}
            onEdit={setEditIssue}
            onAddChild={openCreateChild}
            onToggleExpand={toggleExpand}
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
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, p: 1.5 }}>
            {GROUP_ORDER.map((g) => {
              const arr = grouped[g];
              if (arr.length === 0) {
                return null;
              }
              const isCollapsed = collapsed.has(g);
              return (
                <Paper
                  key={g}
                  variant="outlined"
                  sx={{ display: 'flex', flexDirection: 'column', borderRadius: 1, bgcolor: 'background.default' }}
                >
                  <Box sx={{ px: 2, py: 1, cursor: 'pointer' }} onClick={() => toggleGroup(g)}>
                    <StateGroupCard
                      leading={isCollapsed
                        ? <ExpandMoreOutlinedIcon fontSize="small" color="action" />
                        : <ExpandLessOutlinedIcon fontSize="small" color="action" />}
                      color={stateMap.get(firstStateIdByGroup[g] ?? -1)?.color}
                      name={t(`tracker:projectIssue.group.${g}`)}
                      count={arr.length}
                      onAdd={() => openCreate(firstStateIdByGroup[g])}
                    />
                  </Box>
                  <Collapse in={!isCollapsed}>
                    <Box sx={{ p: 1, pt: 0 }}>
                      {arr.map(projectIssue => (
                        <IssueCard
                          key={projectIssue.id}
                          issue={projectIssue}
                          stateMap={stateMap}
                          subtaskStats={subtaskStats}
                          childIssues={childrenByParent.get(projectIssue.id) ?? []}
                          expanded={expandedParents.has(projectIssue.id)}
                          onToggleExpand={toggleExpand}
                          onEdit={setEditIssue}
                          onAddChild={openCreateChild}
                        />
                      ))}
                    </Box>
                  </Collapse>
                </Paper>
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

      {createChildParent && (
        <ProjectIssueDrawer
          mode="create"
          workspaceProject={workspaceProject}
          projectStates={projectStates}
          parentIssue={createChildParent}
          onClose={() => setCreateChildParent(null)}
          onCreated={handleChildCreated}
        />
      )}

      {editIssue && (
        <ProjectIssueDrawer
          mode="edit"
          projectIssue={editIssue}
          workspaceProject={workspaceProject}
          projectStates={projectStates}
          onClose={() => setEditIssue(null)}
          onUpdated={handleUpdated}
          onDeleted={handleDeleted}
        />
      )}
    </Box>
  );
}

export default ProjectIssueList;
