import type { ProjectIssueResponseData, WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import type { ProjectStateView } from '@src/state/tracker';
import { KeyboardArrowDownRounded as KeyboardArrowDownRoundedIcon, KeyboardArrowRightRounded as KeyboardArrowRightRoundedIcon } from '@mui/icons-material';
import { Box, Chip, CircularProgress, List, ListItemButton, ListItemIcon, ListItemText, ListSubheader, Typography } from '@mui/material';
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

const truncateSx = { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' } as const;

// DevTaskTree：开发工作台左任务树——跨所有工作空间展示 started 组的 issue。
// 三级：workspace → project → dev issue。复用 MUI List 组件族（ListSubheader / ListItemButton / Chip）的内置样式，
// 选中态/hover 走 ListItemButton selected（与侧栏一致），最大限度复用 MUI、少手写 sx。
// 数据复用 tracker 缓存；过滤走 filterDevIssues（started 组）。选中 issue → useDevWorkbenchStore。
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
    <List dense component="nav" sx={{ flex: 1, overflow: 'auto', py: 0.5 }}>
      {workspaces.map(ws => <WorkspaceNode key={ws.id} workspace={ws} />)}
    </List>
  );
}

// workspace 段标题（ListSubheader）+ 其下 project 列表。
// 预判是否有 dev issue（useQueries 复用缓存），无则整体不渲染，避免孤立分组头。
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

  const devProjectFlags = projects.map((_, i) => {
    const views = buildStateViews(statesQueries[i].data ?? [], catalog);
    const viewMap = new Map<number, ProjectStateView>();
    views.forEach(v => viewMap.set(v.id, v));
    return filterDevIssues(issuesQueries[i].data ?? [], viewMap).length > 0;
  });
  const isLoading = issuesQueries.some(q => q.isLoading) || statesQueries.some(q => q.isLoading);

  if (projects.length === 0) {
    return null;
  }
  if (!isLoading && !devProjectFlags.some(Boolean)) {
    return null;
  }

  return (
    <>
      <ListSubheader disableSticky sx={{ pt: 1.5, pb: 1.8, color: 'text.secondary', lineHeight: 1.4 }}>
        {workspace.name}
      </ListSubheader>
      {projects.map((p, i) => (devProjectFlags[i] ? <ProjectNode key={p.id} project={p} /> : null))}
    </>
  );
}

// project 折叠头（ListItemButton 内置 hover）+ 计数（Chip）+ 其下 dev issue 行。
function ProjectNode({ project }: { project: WorkspaceProjectModel }) {
  const { data: issues = [] } = useProjectIssues(project.id);
  const { viewMap } = useProjectStateViews(project.id);
  const devIssues = useMemo(() => filterDevIssues(issues, viewMap), [issues, viewMap]);
  const [open, setOpen] = useState(true);

  if (devIssues.length === 0) {
    return null;
  }

  const emoji = project.emoji.trim();
  return (
    <>
      <ListItemButton onClick={() => setOpen(o => !o)} sx={{ mx: 0.5, borderRadius: 1, pr: 1 }}>
        <ListItemIcon sx={{ minWidth: 'auto', justifyContent: 'center', mr: 0.5, color: 'text.secondary' }}>
          {open
            ? <KeyboardArrowDownRoundedIcon fontSize="small" />
            : <KeyboardArrowRightRoundedIcon fontSize="small" />}
        </ListItemIcon>
        <ListItemText primary={emoji ? `${emoji} ${project.name}` : project.name} slotProps={{ primary: { noWrap: true, sx: { fontWeight: 600 } } }} />
        <Chip label={devIssues.length} size="small" />
      </ListItemButton>
      {open && devIssues.map(issue => (
        <DevIssueRow key={issue.id} issue={issue} viewMap={viewMap} />
      ))}
    </>
  );
}

// dev issue 行：ListItemButton selected（内置选中态 + hover，复用主题 selectedColor）+ 子状态徽章（色点+name）。点击选中/取消。
function DevIssueRow({ issue, viewMap }: { issue: ProjectIssueResponseData; viewMap: Map<number, ProjectStateView> }) {
  const selectedIssueId = useDevWorkbenchStore(s => s.selectedIssueId);
  const selectIssue = useDevWorkbenchStore(s => s.selectIssue);
  const selected = selectedIssueId === issue.id;
  const view = viewMap.get(issue.stateId);

  return (
    <ListItemButton selected={selected} onClick={() => selectIssue(selected ? null : issue.id)} sx={{ mx: 0.5, borderRadius: 1 }}>
      {/* 占位箭头：与 project 折叠头的箭头 ListItemIcon 同宽，使任务名与项目名左对齐 */}
      <ListItemIcon sx={{ minWidth: 'auto', mr: 0.5, visibility: 'hidden' }}>
        <KeyboardArrowRightRoundedIcon fontSize="small" />
      </ListItemIcon>
      <ListItemText primary={issue.name} slotProps={{ primary: { noWrap: true } }} />
      {view && (
        <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, flexShrink: 0 }}>
          <Box sx={{ width: 7, height: 7, borderRadius: '50%', bgcolor: view.color || 'text.disabled' }} />
          <Typography variant="caption" sx={{ color: 'text.secondary', ...truncateSx }}>{view.name}</Typography>
        </Box>
      )}
    </ListItemButton>
  );
}
