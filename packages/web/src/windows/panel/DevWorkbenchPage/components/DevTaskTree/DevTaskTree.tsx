import type { SubtaskStats } from '@src/components/issueCard/shared';
import type { BrowserSessionView, ProjectIssueResponseData, WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import { KeyboardArrowDownRounded as KeyboardArrowDownRoundedIcon, KeyboardArrowRightRounded as KeyboardArrowRightRoundedIcon, WorkspacesOutlined as WorkspacesOutlinedIcon } from '@mui/icons-material';
import { Box, Chip, CircularProgress, List, ListItemButton, ListItemIcon, ListItemText, ListSubheader, Tooltip, Typography } from '@mui/material';
import IssueCard from '@src/components/issueCard/IssueCard';
import { ProjectIssueService } from '@src/services';
import { isSessionLive, parseTaskSessionProfile, useBrowserSessionEvents, useBrowserSessionView } from '@src/state/browserSession';
import { filterDevIssues, useDevWorkbenchStore } from '@src/state/devWorkbench';
import { buildSubtaskStats, trackerKeys, useProjectIssues, useWorkspaceProjects, useWorkspaces } from '@src/state/tracker';
import { DEV_IID_PARAM, DEV_PID_PARAM } from '@src/windows/panel/routes';
import { useQueries } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';

/**
 * DevTaskTree：开发工作台左任务树——跨所有工作空间展示非终态（BACKLOG/TODO/IN_PROGRESS）的顶级 issue。三级：workspace → project → dev issue。
 * workspace/project 层级复用 MUI List 组件族（ListSubheader / ListItemButton / Chip）内置样式；issue 卡片复用共享 IssueCard（无拖拽/无操作列/隐藏 id 尾 8 位，点击选中 issue），与项目事项管理列表/看板视觉交互一致。
 * 数据复用 tracker 缓存；过滤走 filterDevIssues（非终态顶级，T3.3 放宽）。选中 issue → useDevWorkbenchStore。
 */
export default function DevTaskTree() {
  const { data: workspaces = [], isLoading } = useWorkspaces();
  useBrowserSessionEvents();
  const { data: browserSnapshot } = useBrowserSessionView();
  // 浏览器会话投影（SSE 驱动缓存，树级单订阅）按任务 id 聚合（任意槽位），卡片徽标直查。
  const taskSessions = useMemo(() => {
    const map = new Map<string, BrowserSessionView[]>();
    for (const row of browserSnapshot?.sessions ?? []) {
      const parsed = parseTaskSessionProfile(row.profile);
      if (parsed == null) {
        continue;
      }
      const list = map.get(parsed.issueId) ?? [];
      list.push(row);
      map.set(parsed.issueId, list);
    }
    return map;
  }, [browserSnapshot]);

  if (isLoading && workspaces.length === 0) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: '16px' }}>
        <CircularProgress size={20} />
      </Box>
    );
  }
  if (workspaces.length === 0) {
    return (
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: '16px' }}>
        <Typography variant="body2" color="text.secondary">暂无任务</Typography>
      </Box>
    );
  }
  return (
    <List dense component="nav" sx={{ flex: 1, overflow: 'auto', py: '4px' }}>
      {workspaces.map(ws => <WorkspaceNode key={ws.id} workspace={ws} taskSessions={taskSessions} />)}
    </List>
  );
}

/**
 * workspace 段标题（ListSubheader）+ 其下 project 列表。
 * 预判是否有 dev issue（useQueries 复用缓存），无则整体不渲染，避免孤立分组头。
 */
function WorkspaceNode({ workspace, taskSessions }: { workspace: WorkspaceModel; taskSessions: Map<string, BrowserSessionView[]> }) {
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
      {/* 工作空间图标占位于项目折叠头 chevron 同列（20px + 4px），两级名称左缘对齐于 32px。 */}
      <ListSubheader disableSticky sx={{ pt: '12px', pb: '14.4px', color: 'text.secondary', lineHeight: 1.4, pl: '8px' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
          <WorkspacesOutlinedIcon sx={{ fontSize: '20px' }} />
          <Box component="span" sx={{ minWidth: 0 }}>{workspace.name}</Box>
        </Box>
      </ListSubheader>
      {projects.map((p, i) => (devProjectFlags[i] ? <ProjectNode key={p.id} project={p} taskSessions={taskSessions} /> : null))}
    </>
  );
}

// project 折叠头（ListItemButton 内置 hover）+ 计数（Chip）+ 其下 dev issue 卡片。
function ProjectNode({ project, taskSessions }: { project: WorkspaceProjectModel; taskSessions: Map<string, BrowserSessionView[]> }) {
  const { data: issues = [] } = useProjectIssues(project.id);
  const devIssues = useMemo(() => filterDevIssues(issues), [issues]);
  // 子任务统计（done/total）供卡片进度小标：从同项目全量 issue 派生（含终态子任务）。
  const subtaskStats = useMemo(() => buildSubtaskStats(issues), [issues]);
  const [open, setOpen] = useState(true);

  if (devIssues.length === 0) {
    return null;
  }

  const emoji = project.emoji.trim();
  return (
    <>
      {/* pl '4px' 覆写 MUI 默认 pl 16px：整列左缘留白收紧一档（项目名文字起于 32px）。 */}
      <ListItemButton onClick={() => setOpen(o => !o)} sx={{ mx: '4px', borderRadius: '4px', pl: '4px', pr: '8px' }}>
        <ListItemIcon sx={{ minWidth: 'auto', justifyContent: 'center', mr: '4px', color: 'text.secondary' }}>
          {open
            ? <KeyboardArrowDownRoundedIcon fontSize="small" />
            : <KeyboardArrowRightRoundedIcon fontSize="small" />}
        </ListItemIcon>
        <ListItemText primary={emoji ? `${emoji} ${project.name}` : project.name} slotProps={{ primary: { noWrap: true, sx: { fontWeight: 600 } } }} />
        <Chip label={devIssues.length} size="small" />
      </ListItemButton>
      {open && devIssues.map(issue => <DevIssueRow key={issue.id} issue={issue} subtaskStats={subtaskStats} sessions={taskSessions.get(issue.id) ?? []} />)}
    </>
  );
}

// dev issue 卡片：复用共享 IssueCard viewScene="devWorkbench"（无拖拽/无操作列/隐藏 id 尾 8 位/三行顶格左对齐，
// onCardClick 选中 issue，选中态高亮边框）。
// 外层 Box 提供层级缩进（ml '16px' = 16px，卡缘居 project 折叠头箭头区间中点：折叠头 mx '4px' +
// pl '4px' + 箭头 20px + mr '4px'，项目名文字起于 32px）。
// 整卡 Tooltip 右侧展示完整名称（左缘元素约定 placement="right"）。点击选中/取消。
function DevIssueRow({ issue, subtaskStats, sessions }: { issue: ProjectIssueResponseData; subtaskStats: SubtaskStats; sessions: BrowserSessionView[] }) {
  const selectedIssueId = useDevWorkbenchStore(s => s.selectedIssueId);
  const selectIssue = useDevWorkbenchStore(s => s.selectIssue);
  const navigate = useNavigate();
  const selected = selectedIssueId === issue.id;
  // 徽标代表行：任一会话 live 优先展示，否则取最低槽位（无会话为 undefined 不出徽标）。
  const bySlot = (a: BrowserSessionView, b: BrowserSessionView) =>
    (parseTaskSessionProfile(a.profile)?.slot ?? 1) - (parseTaskSessionProfile(b.profile)?.slot ?? 1);
  const browserSession = sessions.find(s => isSessionLive(s.status)) ?? [...sessions].sort(bySlot)[0];

  const card = (
    <Box sx={{ mt: '12px', ml: '16px', mr: '8px' }}>
      <IssueCard
        issue={issue}
        viewScene="devWorkbench"
        subtaskStats={subtaskStats}
        browserSession={browserSession}
        selected={selected}
        onCardClick={() => {
          // 选中/取消都同步 URL（pid+iid）+ store（供本卡高亮即时刷新；URL→store 主同步在 DevWorkbenchPage）。
          if (selected) {
            selectIssue(null);
            navigate('/devWorkbench');
          } else {
            selectIssue(issue);
            navigate(`/devWorkbench?${DEV_PID_PARAM}=${issue.projectId}&${DEV_IID_PARAM}=${issue.id}`);
          }
        }}
      />
    </Box>
  );

  return (
    <Tooltip
      title={issue.name}
      placement="right"
      slotProps={{ tooltip: { sx: { maxWidth: '320px', whiteSpace: 'pre-wrap' } } }}
    >
      {card}
    </Tooltip>
  );
}
