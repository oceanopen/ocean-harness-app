import type { ProjectIssueResponseData, WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import type { ProjectStateView } from '@src/state/tracker';
import { KeyboardArrowDownRounded as KeyboardArrowDownRoundedIcon, KeyboardArrowRightRounded as KeyboardArrowRightRoundedIcon } from '@mui/icons-material';
import { Box, CircularProgress, Typography } from '@mui/material';
import { ProjectIssueService, ProjectStateService } from '@src/services';
import { filterDevIssues, useDevWorkbenchStore } from '@src/state/devWorkbench';
import {
  buildStateViews,
  trackerKeys,
  useProjectIssues,
  useProjectStateViews,
  useStateCatalog,
  useWorkspaceProjects,
  useWorkspaces,
} from '@src/state/tracker';
import { useQueries } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

// 单行文本截断（同 ProjectIssueList/shared.ts 的 truncateSx，此处模块级复用，避免跨域 import tracker 内部）。
const truncateSx = { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' } as const;

// DevTaskTree：开发工作台左任务树——跨所有工作空间展示处于开发流程的 issue。
// 三级：workspace → project → dev issue（顶层全展示，一眼看全所有处理中任务，无需切换工作空间）。
// 数据全部复用 tracker React Query 缓存（同 key 命中零请求）；过滤走 filterDevIssues（started 组、非 in_progress）。
// 选中 issue → useDevWorkbenchStore.selectedIssueId，供右栏步骤条（模块 C/D）渲染。
export default function DevTaskTree() {
  const { t } = useTranslation();
  const { data: workspaces = [], isLoading } = useWorkspaces();

  if (isLoading && workspaces.length === 0) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: 2 }}>
        <CircularProgress size={20} />
      </Box>
    );
  }
  if (workspaces.length === 0) {
    return (
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
        <Typography variant="body2" color="text.secondary">{t('panel:devWorkbench.empty')}</Typography>
      </Box>
    );
  }
  return (
    <Box sx={{ flex: 1, overflow: 'auto', py: 0.5 }}>
      {workspaces.map(ws => <WorkspaceNode key={ws.id} workspace={ws} />)}
    </Box>
  );
}

// workspace 分组：预判其下是否有 dev issue（用 useQueries 复用 tracker 缓存），无则整体不渲染，
// 避免「有项目但全无开发中任务」时留下孤立的 workspace 分组头（多数 project 的 issue 不在开发流程，此情况常见）。
function WorkspaceNode({ workspace }: { workspace: WorkspaceModel }) {
  const { data: projects = [] } = useWorkspaceProjects(workspace.id);
  const issuesQueries = useQueries({
    queries: projects.map(p => ({
      queryKey: trackerKeys.projectIssues(p.id),
      queryFn: () => ProjectIssueService.getList({ projectId: p.id }),
    })),
  });
  const statesQueries = useQueries({
    queries: projects.map(p => ({
      queryKey: trackerKeys.projectStates(p.id),
      queryFn: () => ProjectStateService.getList({ projectId: p.id }),
    })),
  });
  const { data: catalog } = useStateCatalog();

  // 预判每个 project 是否有 dev issue（join 目录 → viewMap → filterDevIssues）。
  // 不用 useMemo：useQueries 每次返回新数组引用致 memo 失效；计算轻量（projects/issues 规模小），直接算更清晰。
  const devProjectFlags = projects.map((_, i) => {
    const views = buildStateViews(statesQueries[i].data ?? [], catalog);
    const viewMap = new Map<number, ProjectStateView>();
    views.forEach(v => viewMap.set(v.id, v));
    return filterDevIssues(issuesQueries[i].data ?? [], viewMap).length > 0;
  });
  const isLoading = issuesQueries.some(q => q.isLoading) || statesQueries.some(q => q.isLoading);

  if (projects.length === 0) {
    return null;
  } // 无项目
  if (!isLoading && !devProjectFlags.some(Boolean)) {
    return null;
  } // 有项目但加载完仍无 dev issue

  return (
    <Box sx={{ mb: 0.5 }}>
      <Typography
        variant="caption"
        sx={{ display: 'block', px: 1.5, py: 0.5, color: 'text.disabled', fontWeight: 700, ...truncateSx }}
      >
        {workspace.name}
      </Typography>
      {projects.map((p, i) => (devProjectFlags[i] ? <ProjectNode key={p.id} project={p} /> : null))}
    </Box>
  );
}

// project 子分组：可折叠；标题（emoji+名+计数）+ 其下 dev issue 行。
function ProjectNode({ project }: { project: WorkspaceProjectModel }) {
  const { data: issues = [] } = useProjectIssues(project.id);
  const { viewMap } = useProjectStateViews(project.id);
  const devIssues = useMemo(() => filterDevIssues(issues, viewMap), [issues, viewMap]);
  const [open, setOpen] = useState(true);

  if (devIssues.length === 0) {
    return null;
  } // 双保险（WorkspaceNode 已预判，此处防数据竞态）

  const emoji = project.emoji.trim();
  return (
    <Box sx={{ mb: 0.25 }}>
      <Box
        onClick={() => setOpen(o => !o)}
        sx={[
          { display: 'flex', alignItems: 'center', gap: 0.5, mx: 0.5, px: 1, py: 0.25, cursor: 'pointer', borderRadius: 1 },
          { '&:hover': { bgcolor: 'action.hover' } },
        ]}
      >
        {open
          ? <KeyboardArrowDownRoundedIcon fontSize="small" color="disabled" />
          : <KeyboardArrowRightRoundedIcon fontSize="small" color="disabled" />}
        {emoji && (
          <Typography component="span" sx={{ fontSize: '0.9rem', lineHeight: 1 }}>{emoji}</Typography>
        )}
        <Typography variant="caption" sx={{ fontWeight: 600, flex: 1, minWidth: 0, ...truncateSx }}>{project.name}</Typography>
        <Typography variant="caption" color="text.disabled" sx={{ flexShrink: 0 }}>{devIssues.length}</Typography>
      </Box>
      {open && devIssues.map(issue => (
        <DevIssueRow key={issue.id} issue={issue} viewMap={viewMap} />
      ))}
    </Box>
  );
}

// dev issue 行：选中态（左侧主色边条 + 底色）+ 标题 + 子状态徽章（色点+name）。点击选中。
function DevIssueRow({ issue, viewMap }: { issue: ProjectIssueResponseData; viewMap: Map<number, ProjectStateView> }) {
  const selectedIssueId = useDevWorkbenchStore(s => s.selectedIssueId);
  const selectIssue = useDevWorkbenchStore(s => s.selectIssue);
  const selected = selectedIssueId === issue.id;
  const view = viewMap.get(issue.stateId);

  return (
    <Box
      onClick={() => selectIssue(selected ? null : issue.id)}
      sx={[
        {
          display: 'flex',
          alignItems: 'center',
          gap: 0.75,
          mx: 0.5,
          pl: 3,
          pr: 1,
          py: 0.5,
          cursor: 'pointer',
          borderRadius: 1,
          borderLeft: 3,
          borderColor: selected ? 'primary.main' : 'transparent',
          bgcolor: selected ? 'action.selected' : 'transparent',
        },
        { '&:hover': { bgcolor: selected ? 'action.selected' : 'action.hover' } },
      ]}
    >
      <Typography variant="body2" sx={{ flex: 1, minWidth: 0, ...truncateSx }}>{issue.name}</Typography>
      {view && (
        <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, flexShrink: 0 }}>
          <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: view.color || 'text.disabled' }} />
          <Typography variant="caption" sx={{ color: 'text.secondary', ...truncateSx }}>{view.name}</Typography>
        </Box>
      )}
    </Box>
  );
}
