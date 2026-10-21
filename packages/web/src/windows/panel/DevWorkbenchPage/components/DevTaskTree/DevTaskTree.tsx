import type { BrowserSessionView, WorkspaceIssueResponseData, WorkspaceModel } from '@src/services';
import { WorkspacesOutlined as WorkspacesOutlinedIcon } from '@mui/icons-material';
import { Box, CircularProgress, List, ListSubheader, Tooltip, Typography } from '@mui/material';
import IssueCard from '@src/components/issueCard/IssueCard';
import { deriveIssueProfile, useBrowserSessionEvents, useBrowserSessionView } from '@src/state/browserSession';
import { filterDevIssues, useDevWorkbenchStore } from '@src/state/devWorkbench';
import { useWorkspaceIssues, useWorkspaces } from '@src/state/tracker';
import { DEV_IID_PARAM, DEV_WID_PARAM } from '@src/windows/panel/routes';
import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';

/**
 * DevTaskTree：开发工作台左任务树——跨所有工作空间展示非终态（BACKLOG/TODO/IN_PROGRESS）任务。两级：workspace → dev 任务。
 * workspace 段标题复用 MUI ListSubheader；任务卡片复用共享 IssueCard（无拖拽/无操作列/隐藏 id 尾 8 位，点击选中任务），
 * 与任务管理列表/看板视觉交互一致。数据复用 tracker 缓存；过滤走 filterDevIssues（非终态）。选中任务 → useDevWorkbenchStore。
 */
export default function DevTaskTree() {
  const { data: workspaces = [], isLoading } = useWorkspaces();
  // 浏览器会话投影（SSE 驱动缓存，树级单订阅）按 profile 键映射，卡片徽标按任务派生 profile 直查。
  useBrowserSessionEvents();
  const { data: browserSnapshot } = useBrowserSessionView();
  const browserSessions = useMemo(
    () => new Map((browserSnapshot?.sessions ?? []).map(row => [row.profile, row])),
    [browserSnapshot],
  );

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
      {workspaces.map(ws => <WorkspaceNode key={ws.id} workspace={ws} browserSessions={browserSessions} />)}
    </List>
  );
}

/**
 * workspace 段标题 + 其下 dev 任务卡片。预判是否有 dev 任务（与任务管理页同 query 缓存，命中即零请求），
 * 无则整体不渲染，避免孤立分组头。
 */
function WorkspaceNode({ workspace, browserSessions }: { workspace: WorkspaceModel; browserSessions: Map<string, BrowserSessionView> }) {
  const { data: issues = [] } = useWorkspaceIssues(workspace.id);
  const devIssues = useMemo(() => filterDevIssues(issues), [issues]);

  if (devIssues.length === 0) {
    return null;
  }

  const emoji = workspace.emoji.trim();
  return (
    <>
      {/* 图标恒为空间叠层 icon（GUTTER 同列 20px），emoji 有值时以文字前缀形式随名展示。 */}
      <ListSubheader disableSticky sx={{ pt: '12px', pb: '14.4px', color: 'text.secondary', lineHeight: 1.4, pl: '8px' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
          <WorkspacesOutlinedIcon sx={{ fontSize: '20px' }} />
          <Box component="span" sx={{ minWidth: 0 }}>{emoji ? `${emoji} ${workspace.name}` : workspace.name}</Box>
        </Box>
      </ListSubheader>
      {devIssues.map(issue => (
        <DevIssueRow key={issue.id} issue={issue} browserSession={browserSessions.get(deriveIssueProfile(issue.id))} />
      ))}
    </>
  );
}

// dev 任务卡片：复用共享 IssueCard viewScene="devWorkbench"（无拖拽/无操作列/隐藏 id 尾 8 位/三行顶格左对齐，
// onCardClick 选中任务，选中态高亮边框）。
// 外层 Box 提供层级缩进（ml '16px'，卡缘居段标题图标区间中点，任务与空间名视觉分层）。
// 整卡 Tooltip 右侧展示完整名称（左缘元素约定 placement="right"）。点击选中/取消。
function DevIssueRow({ issue, browserSession }: { issue: WorkspaceIssueResponseData; browserSession?: BrowserSessionView }) {
  const selectedIssueId = useDevWorkbenchStore(s => s.selectedIssueId);
  const selectIssue = useDevWorkbenchStore(s => s.selectIssue);
  const navigate = useNavigate();
  const selected = selectedIssueId === issue.id;

  const card = (
    <Box sx={{ mt: '12px', ml: '16px', mr: '8px' }}>
      <IssueCard
        issue={issue}
        viewScene="devWorkbench"
        browserSession={browserSession}
        selected={selected}
        onCardClick={() => {
          // 选中/取消都同步 URL（wid+iid）+ store（供本卡高亮即时刷新；URL→store 主同步在 DevWorkbenchPage）。
          if (selected) {
            selectIssue(null);
            navigate('/devWorkbench');
          } else {
            selectIssue(issue);
            navigate(`/devWorkbench?${DEV_WID_PARAM}=${issue.workspaceId}&${DEV_IID_PARAM}=${issue.id}`);
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
