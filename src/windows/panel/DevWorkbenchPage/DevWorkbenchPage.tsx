import type { IssueWorkspaceArchiveAction, ProjectIssueResponseData } from '@src/services';
import {
  CleaningServices as CleaningServicesIcon,
  MoreHoriz as MoreHorizIcon,
  ViewSidebar as ViewSidebarIcon,
  ViewSidebarOutlined as ViewSidebarOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Menu,
  MenuItem,
  Tooltip,
  Typography,
  useTheme,
} from '@mui/material';
import {
  decodeWorkspaceBaseDir,
  DEFAULT_PANEL_DEV_TOOL_AREA_COLLAPSED,
  DEFAULT_PANEL_DEV_TOOL_AREA_WIDTH,
  DEFAULT_PANEL_DEV_TREE_COLLAPSED,
  DEFAULT_WORKSPACE_BASE_DIR,
  isYes,
  PANEL_DEV_TOOL_AREA_COLLAPSED_KEY,
  PANEL_DEV_TOOL_AREA_WIDTH_KEY,
  PANEL_DEV_TREE_COLLAPSED_KEY,
  parseYesNo,
  setAppConfig,
  toYesNo,
  WORKSPACE_BASE_DIR_KEY,
} from '@src/shared/appConfig';
import { useConfigReady } from '@src/shared/useConfigReady';
import { useConfigValue } from '@src/shared/useConfigValue';
import { useToast } from '@src/shared/useToast';
import { useDevWorkbenchStore } from '@src/state/devWorkbench';
import { useArchiveIssueWorkspace, useInitIssueWorkspace } from '@src/state/issueWorkspace';
import { removeLayout } from '@src/state/terminalPanes';
import { STATE_MAP, useProjectIssues } from '@src/state/tracker';
import { clearToolTabs } from '@src/state/workbenchTools';
import { DEV_IID_PARAM, DEV_PID_PARAM, numParam, strParam } from '@src/windows/panel/routes';
import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import DevTaskTree from './components/DevTaskTree/DevTaskTree';
import TerminalErrorBoundary from './components/EmbeddedTerminal/TerminalErrorBoundary';
import TerminalPaneRoot from './components/TerminalPanes/TerminalPaneRoot';
import TerminalSplitButtons from './components/TerminalPanes/TerminalSplitButtons';
import ToolPanelArea, { TERMINAL_MIN_WIDTH, TOOL_AREA_MIN_WIDTH } from './components/WorkbenchTools/ToolPanelArea';
import WorkbenchToolRail from './components/WorkbenchTools/WorkbenchToolRail';
import WorkspaceInitGate from './components/WorkspaceInitGate/WorkspaceInitGate';

// 左栏折叠状态 decode：缺失/非法值回落到默认（展开）。
// 模块级函数保证引用稳定（useConfigValue 依赖项要求，避免每次渲染重订阅）。
function decodeDevTreeCollapsed(raw: string | null): boolean {
  return isYes(parseYesNo(raw, DEFAULT_PANEL_DEV_TREE_COLLAPSED));
}

// 右侧工具面板区折叠状态 decode：同左栏范式（默认收起）。
function decodeToolAreaCollapsed(raw: string | null): boolean {
  return isYes(parseYesNo(raw, DEFAULT_PANEL_DEV_TOOL_AREA_COLLAPSED));
}

// 工具面板区宽度 decode：非法/越界回落默认 600。上界 2400 仅防 DB 手改乱值——
// 运行时实际上限由拖拽按容器实测宽收紧（ToolPanelArea）。
function decodeToolAreaWidth(raw: string | null): number {
  const parsed = raw != null ? Number.parseInt(raw, 10) : Number.NaN;
  if (!Number.isFinite(parsed) || parsed < TOOL_AREA_MIN_WIDTH || parsed > 2400) {
    return DEFAULT_PANEL_DEV_TOOL_AREA_WIDTH;
  }
  return parsed;
}

// 布局挂载前置配置集：左栏折叠 + 工具面板区折叠/宽度三个 key 决定各栏首帧几何。
// 就绪前渲染会按默认态布局、配置到达后动画纠正——每次挂载翻转一遍，中栏终端区
// 逐帧 resize 产生 SIGWINCH 重绘伪影。就绪后首帧即终值（过渡动画本身保留，仅手动
// 开合时触发）。模块级常量保证引用稳定（useConfigReady 要求）。
const PANEL_LAYOUT_CONFIG_KEYS: readonly string[] = [
  PANEL_DEV_TREE_COLLAPSED_KEY,
  PANEL_DEV_TOOL_AREA_COLLAPSED_KEY,
  PANEL_DEV_TOOL_AREA_WIDTH_KEY,
];

// DevWorkbenchPage：控制台「开发工作台」骨架页。
// 左中右布局：左栏任务树（非终态顶级 issue，T3.3 放宽——BACKLOG/TODO/IN_PROGRESS 全生命周期）｜中栏 = 终端列（标题栏 + 终端区）+ 工具
// 面板区（tab 化，ToolPanelArea，tab 头与标题栏同带对齐）｜最右常驻工具条（WorkbenchToolRail，
// 顶部方格 = 面板区总开关，下方工具图标，注册表驱动扩展——后续浏览器/文件目录见
// toolRegistry）。工具 tabs 按 issue 隔离（workbenchTools 域 + localStorage），面板区
// 折叠/宽度走 config 持久化。
//
// 路由接入（全 query 风格）：issue 选中由 URL 驱动——
//   ?pid=<projectId>&iid=<issueId>   选中 issue（项目→issue，issue 靠 project 加载，故 pid 同在 URL）
// 本页单向同步 URL→store。store 全局，页面卸载/重挂载（声明式路由切走即卸载）不丢选中。
export default function DevWorkbenchPage() {
  const theme = useTheme();
  const [searchParams, setSearchParams] = useSearchParams();
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

  // 右侧工具面板区折叠态（工具条顶部方格开关）：同左栏范式，config 持久化（跨重启、多窗口同步）。
  const toolAreaCollapsed = useConfigValue(PANEL_DEV_TOOL_AREA_COLLAPSED_KEY, decodeToolAreaCollapsed, true);
  const toggleToolAreaCollapsed = () => {
    void setAppConfig(PANEL_DEV_TOOL_AREA_COLLAPSED_KEY, toYesNo(!toolAreaCollapsed));
  };
  // 工具面板区宽度：拖拽结束落盘（ToolPanelArea up 回调），拖拽期纯组件内存态。
  const toolAreaWidth = useConfigValue(PANEL_DEV_TOOL_AREA_WIDTH_KEY, decodeToolAreaWidth, DEFAULT_PANEL_DEV_TOOL_AREA_WIDTH);
  const commitToolAreaWidth = (width: number) => {
    void setAppConfig(PANEL_DEV_TOOL_AREA_WIDTH_KEY, String(width));
  };

  // 工作空间根目录（issueWorkspace 初始化闸门与右上角重新初始化按钮共用；空串 = 未设置）。
  const baseDir = useConfigValue(WORKSPACE_BASE_DIR_KEY, decodeWorkspaceBaseDir, DEFAULT_WORKSPACE_BASE_DIR);
  const initWorkspace = useInitIssueWorkspace();

  // 归档/取消（T3.2）：⋯ 菜单入口 + 两段式确认（首确认 → 后端安全检查 → 警告态强确认）。
  const { show: showToast, snack: toastSnack } = useToast();
  const archiveWorkspace = useArchiveIssueWorkspace();
  const [actionsMenuAnchor, setActionsMenuAnchor] = useState<HTMLElement | null>(null);
  // 确认弹窗态：null = 关闭；warnings = null 首确认态，非空 = 安全检查警告态（二次确认）。
  const [archiveConfirm, setArchiveConfirm] = useState<{ kind: IssueWorkspaceArchiveAction; warnings: string[] | null } | null>(null);

  // 加载用 pid：URL 优先（reload/恢复），否则 store（URL 无 dev 参数的兜底）；null 时不发请求。
  const loadPid = urlPid ?? selectedProjectId;
  const { data: issues = [], isLoading: issuesLoading } = useProjectIssues(loadPid);

  // 有效选中：URL 优先，否则 store。
  const effIssueId = urlIid ?? selectedIssueId;
  const hasSelection = effIssueId != null && loadPid != null;
  const issue = issues.find(i => i.id === effIssueId);
  const stateMeta = issue ? STATE_MAP.get(issue.stateCode) : undefined;

  // 提交归档/取消：首确认（warnings=null → force=false，后端干净则内部续发执行段）/
  // 警告态强确认（warnings 非空 → force=true 只走执行段）。成功后按序清理：该 issue 本地
  // 痕迹（工具 tabs + 终端分屏布局，防 localStorage 积脏 key）→ 清选中 + 清 URL
  // （URL→store 同步 effect 会从 iid 恢复选中，双清才彻底，参照 DevIssueRow 取消选中）。
  const submitArchive = () => {
    if (archiveConfirm == null || issue == null || loadPid == null) {
      return;
    }
    const { kind, warnings } = archiveConfirm;
    archiveWorkspace.mutate(
      { projectId: loadPid, issueId: issue.id, baseDir, action: kind, force: warnings != null },
      {
        onSuccess: (result) => {
          if (result.status === 'warnings') {
            setArchiveConfirm({ kind, warnings: result.warnings });
            return;
          }
          setArchiveConfirm(null);
          showToast(kind === 'archive' ? '已归档：工作空间目录已删除' : '已取消：工作空间目录已删除', 'success');
          clearToolTabs(issue.id);
          removeLayout(issue.id);
          selectIssue(null);
          setSearchParams({}, { replace: true });
        },
        onError: (e) => {
          showToast(e instanceof Error ? e.message : String(e), 'error');
        },
      },
    );
  };

  // 工具面板区可见性：用户展开（config）且选中 issue 有效（面板内容均围绕选中任务）。
  const toolAreaVisible = !toolAreaCollapsed && hasSelection && issue != null;

  // 三栏折叠态配置就绪闸门（见 PANEL_LAYOUT_CONFIG_KEYS 注释）：就绪后才渲染
  // 布局，首帧即终值。一次性等待（本地 IPC，~ms 级）。
  const panelConfigReady = useConfigReady(PANEL_LAYOUT_CONFIG_KEYS);

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

  // 折叠态配置未就绪：空占位，避免首帧默认展开 → 动画收起的宽度翻转。
  if (!panelConfigReady) {
    return <Box sx={{ height: '100%' }} />;
  }

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

      {/* 中栏 = 内容行：终端列（含标题栏）+ 工具面板区（tab 头与标题栏同带对齐）。
          标题栏下沉终端列——操作组（ml:auto）落终端列右缘：面板展开时紧贴其左边界，不被推到最右。 */}
      <Box sx={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* 终端列：标题栏 + 终端内容；minWidth 与工具面板区拖拽上限联动（ToolPanelArea 按容器实测宽收紧 max） */}
        <Box sx={{ flex: 1, minWidth: TERMINAL_MIN_WIDTH, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
          {/* 标题栏：左栏折叠开关 + 状态徽章 + 选中 issue 的 id 尾 8 位 + 名称 + 右侧快捷区（终端分割） */}
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
              {/* 清理终端并重新初始化（T1.5）：清理该 issue 全部终端会话后走增量初始化（已成功步骤/仓库跳过），
                与 WorkspaceInitGate 面板按钮共用同一 mutation/query key，面板自动切换回进度态。
                图标用扫帚（CleaningServices）而非循环箭头，与子任务面板刷新按钮（Autorenew）区分。 */}
              {hasSelection && issue && (
                <Tooltip title="清理终端并重新初始化">
                  <span>
                    <IconButton
                      size="small"
                      aria-label="清理终端并重新初始化"
                      disabled={initWorkspace.isPending || baseDir === ''}
                      onClick={() => initWorkspace.mutate({ issueId: issue.id, baseDir })}
                      sx={{ color: 'text.secondary' }}
                    >
                      {/* 满幅实心构图光学尺寸偏大，字号 16（small 默认 18）与轻笔画邻居平衡 */}
                      <CleaningServicesIcon sx={{ fontSize: 16 }} />
                    </IconButton>
                  </span>
                </Tooltip>
              )}
              {/* 终端分割按钮组（作用于活跃 pane；原终端区工具条上移至此） */}
              {hasSelection && issue && <TerminalSplitButtons issueId={issue.id} />}
              {/* 任务操作菜单（T3.2）：⋯ 入口，归档/取消（删工作空间目录 + 流转 issue 状态，两段式确认） */}
              {hasSelection && issue && loadPid != null && (
                <>
                  <IconButton
                    size="small"
                    aria-label="更多任务操作"
                    onClick={e => setActionsMenuAnchor(e.currentTarget)}
                    sx={{ color: 'text.secondary' }}
                  >
                    <MoreHorizIcon />
                  </IconButton>
                  <Menu anchorEl={actionsMenuAnchor} open={actionsMenuAnchor != null} onClose={() => setActionsMenuAnchor(null)}>
                    <MenuItem
                      onClick={() => {
                        setActionsMenuAnchor(null);
                        setArchiveConfirm({ kind: 'archive', warnings: null });
                      }}
                    >
                      归档任务…
                    </MenuItem>
                    <MenuItem
                      onClick={() => {
                        setActionsMenuAnchor(null);
                        setArchiveConfirm({ kind: 'cancel', warnings: null });
                      }}
                    >
                      取消任务…
                    </MenuItem>
                  </Menu>
                </>
              )}
            </Box>
          </Box>

          {/* 终端内容：选中 issue → 终端 split 树容器（一 issue 一布局，多 pane 各自独立
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
                    // 初始化闸门（T1.5）：三段式引导面板占位终端列，工作空间 SUCCESS 才渲染终端；
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
        <ToolPanelArea
          issue={issue ?? null}
          projectId={loadPid}
          visible={toolAreaVisible}
          width={toolAreaWidth}
          onWidthCommit={commitToolAreaWidth}
        />
      </Box>

      {/* 最右常驻工具条：顶部方格（面板区总开关）+ 工具图标列（注册表驱动）。无选中时禁用。 */}
      <WorkbenchToolRail
        issueId={issue?.id ?? null}
        panelCollapsed={toolAreaCollapsed}
        onTogglePanel={toggleToolAreaCollapsed}
        onExpandPanel={() => void setAppConfig(PANEL_DEV_TOOL_AREA_COLLAPSED_KEY, toYesNo(false))}
      />

      {/* 归档/取消确认（T3.2）：首确认（后果说明）→ 后端安全检查 → 警告态强确认（force 执行） */}
      <Dialog
        open={archiveConfirm != null}
        onClose={archiveWorkspace.isPending ? undefined : () => setArchiveConfirm(null)}
        maxWidth="xs"
        fullWidth
      >
        <DialogTitle>{archiveConfirm?.kind === 'cancel' ? '取消任务' : '归档任务'}</DialogTitle>
        <DialogContent>
          <Typography variant="body2">
            {archiveConfirm?.kind === 'cancel'
              ? '取消将删除该任务的工作空间目录（含全部仓库的本地修改与未推送提交）、关闭其终端会话，issue 置为「已取消」。'
              : '归档将删除该任务的工作空间目录（含全部仓库的本地修改与未推送提交）、关闭其终端会话，issue 置为「已完成」。'}
          </Typography>
          {archiveConfirm?.warnings != null && (
            <Alert severity="warning" sx={{ mt: 1.5 }}>
              安全检查发现以下问题，执行后这些内容将丢失：
              <Box component="ul" sx={{ my: 0.5, pl: 2 }}>
                {archiveConfirm.warnings.map(w => <li key={w}>{w}</li>)}
              </Box>
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button size="small" onClick={() => setArchiveConfirm(null)} disabled={archiveWorkspace.isPending}>返回</Button>
          <Button
            size="small"
            variant="contained"
            color={archiveConfirm?.warnings != null ? 'error' : 'primary'}
            onClick={submitArchive}
            disabled={archiveWorkspace.isPending}
          >
            {archiveWorkspace.isPending
              ? '执行中…'
              : archiveConfirm?.warnings != null
                ? '确认删除工作空间'
                : archiveConfirm?.kind === 'cancel' ? '取消任务' : '归档'}
          </Button>
        </DialogActions>
      </Dialog>
      {toastSnack}
    </Box>
  );
}
