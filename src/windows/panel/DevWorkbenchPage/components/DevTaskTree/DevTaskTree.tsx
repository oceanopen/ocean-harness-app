import type { ProjectIssueResponseData, WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import { CalendarMonthOutlined as CalendarMonthOutlinedIcon, KeyboardArrowDownRounded as KeyboardArrowDownRoundedIcon, KeyboardArrowRightRounded as KeyboardArrowRightRoundedIcon } from '@mui/icons-material';
import { Box, Chip, CircularProgress, List, ListItemButton, ListItemIcon, ListItemText, ListSubheader, Tooltip, Typography } from '@mui/material';
import { ProjectIssueService } from '@src/services';
import { filterDevIssues, useDevWorkbenchStore } from '@src/state/devWorkbench';
import { STATE_MAP, trackerKeys, useProjectIssues, useWorkspaceProjects, useWorkspaces } from '@src/state/tracker';
import { DEV_IID_PARAM, DEV_PID_PARAM } from '@src/windows/panel/routes';
import { useQueries } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';

// DevTaskTree：开发工作台左任务树——跨所有工作空间展示非终态（BACKLOG/TODO/IN_PROGRESS）的顶级 issue。
// 三级：workspace → project → dev issue。workspace/project 层级复用 MUI List 组件族（ListSubheader / ListItemButton / Chip）内置样式，
// issue 行为 Paper 边框卡片（视觉对齐 TrackerPage IssueCard），选中态高亮边框。
// 数据复用 tracker 缓存；过滤走 filterDevIssues（非终态顶级，T3.3 放宽）。选中 issue → useDevWorkbenchStore。
export default function DevTaskTree() {
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
        <Typography variant="body2" color="text.secondary">暂无任务</Typography>
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

  const devProjectFlags = projects.map((_, i) => filterDevIssues(issuesQueries[i].data ?? []).length > 0);
  const isLoading = issuesQueries.some(q => q.isLoading);

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
  const devIssues = useMemo(() => filterDevIssues(issues), [issues]);
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
      {open && devIssues.map(issue => <DevIssueRow key={issue.id} issue={issue} />)}
    </>
  );
}

// dev issue 卡片：视觉对齐 TrackerPage IssueCard（Paper 边框卡片 + 「状态:●」徽章 + 日历日期），
// 两行——首行名称（截断），次行 [状态徽章 … 目标日期（逾期红）]。卡片上下留间隔（my）；选中态高亮边框。
// 整行 Tooltip 右侧展示完整描述（左缘元素约定 placement="right"）。点击选中/取消。
function DevIssueRow({ issue }: { issue: ProjectIssueResponseData }) {
  const selectedIssueId = useDevWorkbenchStore(s => s.selectedIssueId);
  const selectIssue = useDevWorkbenchStore(s => s.selectIssue);
  const navigate = useNavigate();
  const selected = selectedIssueId === issue.id;
  const stateMeta = STATE_MAP.get(issue.stateCode);
  // 打开时刻冻结的"现在"，用于逾期判断（与 IssueCard 同款判定；filterDevIssues 已滤除非终态，无需再判状态）。
  const [now] = useState(() => Date.now());
  const overdue = !!issue.targetDate && new Date(issue.targetDate).getTime() < now;

  const row = (
    <Box
      onClick={() => {
        // 选中/取消都同步 URL（pid+iid）+ store（供本卡高亮即时刷新；URL→store 主同步在 DevWorkbenchPage）。
        if (selected) {
          selectIssue(null);
          navigate('/devWorkbench');
        } else {
          selectIssue(issue);
          navigate(`/devWorkbench?${DEV_PID_PARAM}=${issue.projectId}&${DEV_IID_PARAM}=${issue.id}`);
        }
      }}
      sx={[
        {
          display: 'flex',
          flexDirection: 'column',
          gap: 0.75,
          // 左侧留白：卡片左缘与 project 折叠头的项目名对齐（mx 0.5 + 箭头 20px + mr 0.5 = 28px），标明层级归属。
          ml: 3.5,
          mr: 0.8,
          my: 1,
          p: 1.25,
          borderRadius: 1,
          bgcolor: 'background.paper',
          border: 1,
          borderColor: selected ? 'primary.main' : 'divider',
          cursor: 'pointer',
        },
        { '&:hover': { bgcolor: 'action.hover' } },
      ]}
    >
      <Typography variant="body2" noWrap sx={{ minWidth: 0 }}>{issue.name}</Typography>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 1 }}>
        {stateMeta && (
          <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, minWidth: 0 }}>
            <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>「</Typography>
            <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>状态:</Typography>
            <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: stateMeta.color, flexShrink: 0 }} />
            <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1 }} noWrap>{stateMeta.name}</Typography>
            <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>」</Typography>
          </Box>
        )}
        {!!issue.targetDate && (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexShrink: 0, color: overdue ? 'error.main' : 'text.disabled' }}>
            <CalendarMonthOutlinedIcon sx={{ fontSize: '0.9rem' }} />
            <Typography variant="caption" color="inherit">{issue.targetDate}</Typography>
          </Box>
        )}
      </Box>
    </Box>
  );

  return (
    <Tooltip
      title={issue.name}
      placement="right"
      slotProps={{ tooltip: { sx: { maxWidth: 320, whiteSpace: 'pre-wrap' } } }}
    >
      {row}
    </Tooltip>
  );
}
