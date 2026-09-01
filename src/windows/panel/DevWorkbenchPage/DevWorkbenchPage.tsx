import type { ProjectIssueResponseData } from '@src/services';
import {
  RestartAlt as RestartAltIcon,
  ViewSidebar as ViewSidebarIcon,
  ViewSidebarOutlined as ViewSidebarOutlinedIcon,
} from '@mui/icons-material';
import { Box, Chip, CircularProgress, IconButton, Tooltip, Typography, useTheme } from '@mui/material';
import {
  decodeWorkspaceBaseDir,
  DEFAULT_PANEL_DEV_TREE_COLLAPSED,
  DEFAULT_WORKSPACE_BASE_DIR,
  isYes,
  PANEL_DEV_TREE_COLLAPSED_KEY,
  parseYesNo,
  setAppConfig,
  toYesNo,
  WORKSPACE_BASE_DIR_KEY,
} from '@src/shared/appConfig';
import { useConfigValue } from '@src/shared/useConfigValue';
import { useDevWorkbenchStore } from '@src/state/devWorkbench';
import { useInitIssueWorkspace } from '@src/state/issueWorkspace';
import { STATE_MAP, useProjectIssues } from '@src/state/tracker';
import { DEV_IID_PARAM, DEV_PID_PARAM, numParam, strParam } from '@src/windows/panel/routes';
import { useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import DevTaskTree from './components/DevTaskTree/DevTaskTree';
import TerminalErrorBoundary from './components/EmbeddedTerminal/TerminalErrorBoundary';
import TerminalPaneRoot from './components/TerminalPanes/TerminalPaneRoot';
import TerminalSplitButtons from './components/TerminalPanes/TerminalSplitButtons';
import WorkspaceInitGate from './components/WorkspaceInitGate/WorkspaceInitGate';

// 左栏折叠状态 decode：缺失/非法值回落到默认（展开）。
// 模块级函数保证引用稳定（useConfigValue 依赖项要求，避免每次渲染重订阅）。
function decodeDevTreeCollapsed(raw: string | null): boolean {
  return isYes(parseYesNo(raw, DEFAULT_PANEL_DEV_TREE_COLLAPSED));
}

// DevWorkbenchPage：控制台「开发工作台」骨架页。
// 原固定开发步骤流程（init→developing→pull_request→cleanup，基于 started 组子状态）已移除，
// 后续将接入 AI 驱动开发流程。当前保留：左栏任务树（IN_PROGRESS 的 issue）+ 右栏顶部信息栏与空态提示。
//
// 路由接入（全 query 风格）：issue 选中由 URL 驱动——
//   ?pid=<projectId>&iid=<issueId>   选中 issue（项目→issue，issue 靠 project 加载，故 pid 同在 URL）
// 本页单向同步 URL→store。store 全局，页面卸载/重挂载（声明式路由切走即卸载）不丢选中。
export default function DevWorkbenchPage() {
  const theme = useTheme();
  const [searchParams] = useSearchParams();
  const urlPid = numParam(searchParams.get(DEV_PID_PARAM));
  const urlIid = strParam(searchParams.get(DEV_IID_PARAM)); // issue id 为 uuid 字符串

  const selectedIssueId = useDevWorkbenchStore(s => s.selectedIssueId);
  const selectedProjectId = useDevWorkbenchStore(s => s.selectedProjectId);
  const selectIssue = useDevWorkbenchStore(s => s.selectIssue);
  // 左栏 issue 任务树折叠态：订阅 config（跨重启持久化、多窗口同步，参照 PanelApp 侧边栏）。
  const issueTreeCollapsed = useConfigValue(PANEL_DEV_TREE_COLLAPSED_KEY, decodeDevTreeCollapsed, false);
  const toggleIssueTreeCollapsed = () => {
    void setAppConfig(PANEL_DEV_TREE_COLLAPSED_KEY, toYesNo(!issueTreeCollapsed));
  };

  // 工作空间根目录（issueWorkspace 初始化闸门与右上角重新初始化按钮共用；空串 = 未设置）。
  const baseDir = useConfigValue(WORKSPACE_BASE_DIR_KEY, decodeWorkspaceBaseDir, DEFAULT_WORKSPACE_BASE_DIR);
  const initWorkspace = useInitIssueWorkspace();

  // 加载用 pid：URL 优先（reload/恢复），否则 store（URL 无 dev 参数的兜底）；null 时不发请求。
  const loadPid = urlPid ?? selectedProjectId;
  const { data: issues = [], isLoading: issuesLoading } = useProjectIssues(loadPid);

  // 有效选中：URL 优先，否则 store。
  const effIssueId = urlIid ?? selectedIssueId;
  const hasSelection = effIssueId != null && loadPid != null;
  const issue = issues.find(i => i.id === effIssueId);
  const stateMeta = issue ? STATE_MAP.get(issue.stateCode) : undefined;

  // URL → store 单向同步：有 iid 回写 issue（含其 projectId）；无 iid 清空。
  useEffect(() => {
    if (urlIid == null) {
      if (selectedIssueId != null) {
        selectIssue(null);
      }
    } else if (selectedIssueId !== urlIid) {
      const target = issues.find((i: ProjectIssueResponseData) => i.id === urlIid);
      if (target) {
        selectIssue(target);
      }
    }
  }, [urlIid, issues, selectedIssueId, selectIssue]);

  return (
    <Box sx={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
      {/* 左栏：任务树（workspace→project→dev issue 三级，跨所有工作空间）。
          恒渲染 + width 过渡动画折叠（参照 PanelApp 侧边栏），折叠到 0 后右栏占满整宽。 */}
      <Box
        sx={{
          width: issueTreeCollapsed ? 0 : 260,
          flexShrink: 0,
          borderRight: issueTreeCollapsed ? 0 : 1,
          borderColor: 'divider',
          overflow: 'hidden',
          display: 'flex',
          flexDirection: 'column',
          transition: theme.transitions.create(['width'], {
            duration: theme.transitions.duration.standard,
            easing: theme.transitions.easing.sharp,
          }),
        }}
      >
        <Box sx={{ width: 260, flexShrink: 0, display: 'flex', flexDirection: 'column', height: '100%' }}>
          <DevTaskTree />
        </Box>
      </Box>

      {/* 右栏：顶部操作栏 + 内容区 */}
      <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        {/* 顶部操作栏：左栏折叠开关 + 状态徽章 + 选中 issue 的 id 尾 8 位 + 名称 + 右侧快捷区（终端分割） */}
        <Box
          sx={{
            height: 48,
            flexShrink: 0,
            display: 'flex',
            alignItems: 'center',
            px: 1,
            gap: 0.5,
            borderBottom: 1,
            borderColor: 'divider',
            bgcolor: 'background.paper',
          }}
        >
          <IconButton
            size="small"
            onClick={toggleIssueTreeCollapsed}
            aria-label={issueTreeCollapsed ? '显示任务列表' : '隐藏任务列表'}
            sx={{ color: 'text.secondary' }}
          >
            {issueTreeCollapsed ? <ViewSidebarOutlinedIcon /> : <ViewSidebarIcon />}
          </IconButton>
          {hasSelection && issue && stateMeta && (
            <Chip
              size="small"
              label={stateMeta.name}
              sx={{ bgcolor: `${stateMeta.color}22`, color: stateMeta.color, fontSize: '0.75rem', flexShrink: 0 }}
            />
          )}
          {hasSelection && issue && (
            <Typography variant="subtitle1" noWrap sx={{ fontWeight: 600 }}>
              <Box component="span" sx={{ color: 'text.secondary', fontWeight: 400, fontFamily: 'monospace' }}>
                …{issue.id.slice(-8)}
              </Box>
              {' '}
              {issue.name}
            </Typography>
          )}
          <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: 0.5, flexShrink: 0 }}>
            {/* 重新初始化工作空间（T1.5）：清理该 issue 全部终端会话后走增量初始化（已成功步骤/仓库跳过），
                与 WorkspaceInitGate 面板按钮共用同一 mutation/query key，面板自动切换回进度态。 */}
            {hasSelection && issue && (
              <Tooltip title="重新初始化工作空间">
                <span>
                  <IconButton
                    size="small"
                    aria-label="重新初始化工作空间"
                    disabled={initWorkspace.isPending || baseDir === ''}
                    onClick={() => initWorkspace.mutate({ issueId: issue.id, baseDir })}
                    sx={{ color: 'text.secondary' }}
                  >
                    <RestartAltIcon />
                  </IconButton>
                </span>
              </Tooltip>
            )}
            {/* 终端分割按钮组（作用于活跃 pane；原终端区工具条上移至此） */}
            {hasSelection && issue && <TerminalSplitButtons issueId={issue.id} />}
          </Box>
        </Box>

        {/* 内容区：选中 issue → 终端 split 树容器（一 issue 一布局，多 pane 各自独立
            PTY 会话；切换 issue 即重挂载，后端会话常驻） */}
        <Box sx={{ flex: 1, minHeight: 0 }}>
          {!hasSelection
            ? (
                <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
                  <Typography variant="body2" color="text.secondary">选择左侧任务开始开发</Typography>
                </Box>
              )
            : issue
              ? (
                  // 初始化闸门（T1.5）：三段式引导面板占位内容区，工作空间 SUCCESS 才渲染终端；
                  // key 随 issue 切换重挂载（过渡态/轮询随 key 重建，互不串扰）。
                  <WorkspaceInitGate key={issue.id} issueId={issue.id} baseDir={baseDir}>
                    <TerminalErrorBoundary key={issue.id}>
                      <TerminalPaneRoot issueId={issue.id} />
                    </TerminalErrorBoundary>
                  </WorkspaceInitGate>
                )
              : issuesLoading
                ? (
                    <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
                      <CircularProgress />
                    </Box>
                  )
                : (
                    <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
                      <Typography variant="body2" color="text.secondary">任务不存在或已移出开发流程</Typography>
                    </Box>
                  )}
        </Box>
      </Box>
    </Box>
  );
}
