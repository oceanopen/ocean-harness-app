import type { ConversationModel, IssueWorkspaceArchiveAction, ProjectTaskResponseData, WorkspaceLaunchSettings } from '@src/services';
import type { ReactNode } from 'react';
import type { WorkbenchToolIssue } from './WorkbenchTools/toolRegistry';
import {
  CleaningServices as CleaningServicesIcon,
  CloseFullscreen as CloseFullscreenIcon,
  MoreHoriz as MoreHorizIcon,
  OpenInFull as OpenInFullIcon,
  ViewSidebar as ViewSidebarIcon,
  ViewSidebarOutlined as ViewSidebarOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
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
  DEFAULT_PANEL_DEV_TOOL_AREA_COLLAPSED,
  DEFAULT_PANEL_DEV_TOOL_AREA_WIDTH,
  DEFAULT_PANEL_DEV_TREE_COLLAPSED,
  isYes,
  PANEL_DEV_TOOL_AREA_COLLAPSED_KEY,
  PANEL_DEV_TOOL_AREA_WIDTH_KEY,
  PANEL_DEV_TREE_COLLAPSED_KEY,
  parseYesNo,
  setAppConfig,
  toYesNo,
} from '@src/shared/appConfig';
import { acpAgentLabel, mergeLaunchSettings, terminalAgentLabel } from '@src/shared/launchSettings';
import { isMacOS, TRAFFIC_LIGHT_CLEARANCE } from '@src/shared/platform';
import { useConfigReady } from '@src/shared/useConfigReady';
import { useConfigValue } from '@src/shared/useConfigValue';
import { useToast } from '@src/shared/useToast';
import { useAgentCatalog } from '@src/state/agentCatalog';
import { useConversations } from '@src/state/conversation';
import { useDevWorkbenchStore } from '@src/state/devWorkbench';
import { useArchiveIssueWorkspace, useInitTaskWorkspace } from '@src/state/taskWorkspace';
import { removeLayouts } from '@src/state/terminalPanes';
import { clearToolTabs } from '@src/state/workbenchTools';
import { clearPreviewTabs, useWorkspaceFilesStore } from '@src/state/workspaceFiles';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { useEffect, useState } from 'react';
import { TERMINAL_MIN_WIDTH, TOOL_AREA_MIN_WIDTH, WORKBENCH_TITLEBAR_HEIGHT } from '../workbenchLayout';
import AcpSessionView from './AcpSessionView';
import TerminalErrorBoundary from './EmbeddedTerminal/TerminalErrorBoundary';
import FilePreviewOverlay from './FilePreviewOverlay/FilePreviewOverlay';
import { TerminalLaunchFlow } from './LaunchModePicker';
import OpenWorkspaceDirButton from './OpenWorkspaceDir/OpenWorkspaceDirButton';
import { useTaskMainConversation } from './useTaskMainConversation';
import BrowserStageOverlay from './WorkbenchTools/BrowserPanel/BrowserStageOverlay';
import ToolPanelArea from './WorkbenchTools/ToolPanelArea';
import WorkbenchToolRail from './WorkbenchTools/WorkbenchToolRail';
import WorkspaceInitGate from './WorkspaceInitGate/WorkspaceInitGate';

// 左树折叠状态 decode：缺失/非法值回落到默认（展开）。
// 模块级函数保证引用稳定（useConfigValue 依赖项要求，避免每次渲染重订阅）。
function decodeTreeCollapsed(raw: string | null): boolean {
  return isYes(parseYesNo(raw, DEFAULT_PANEL_DEV_TREE_COLLAPSED));
}

// 右侧工具面板区折叠状态 decode：同左树范式（默认收起）。
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

// 终端列空态占位：居中次要文案。
function CenterNote({ text }: { text: string }) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: '16px' }}>
      <Typography variant="body2" color="text.secondary">{text}</Typography>
    </Box>
  );
}

// 终端列等待态占位：居中 spinner（会话/任务数据就绪闸门）。
function CenterSpinner() {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
      <CircularProgress />
    </Box>
  );
}

/**
 * 生效启动设置 → 终端直启值（T0.3/T0.4，issue 覆盖 + workspace 字段级合并后的单源）：
 * 仅 terminal-auto 直启（值为所选终端启动 Agent）；none / acp / 未配置 = null（裸 shell）。
 */
function terminalStartupCli(mode: 'none' | 'terminal-auto' | 'acp' | undefined, autoCommand: string | undefined): string | null {
  return mode === 'terminal-auto' ? autoCommand ?? 'claude' : null;
}

// 标题栏 IconButton 统一 sx：text.secondary 色调 + icon 尺寸 16px（与终端工具栏
// TOOLBAR_ICON_SX 同档对齐）。尺寸必须走 '& svg' 后代选择器——SvgIcon 的
// width/height = 1em 跟随自身 font-size，由其 variant class 显式设定（缺省
// medium = 24px），在 IconButton 上直接设 fontSize 对 svg 无效（实测教训）。
const TITLEBAR_ICON_SX = { 'color': 'text.secondary', '& svg': { fontSize: '16px' } } as const;

// 布局挂载前置配置集：左树折叠 + 工具面板区折叠/宽度三个 key 决定各栏首帧几何。
// 就绪前渲染会按默认态布局、配置到达后动画纠正——每次挂载翻转一遍，中栏终端区
// 逐帧 resize 产生 SIGWINCH 重绘伪影。就绪后首帧即终值（过渡动画本身保留，仅手动
// 开合时触发）。模块级常量保证引用稳定（useConfigReady 要求）。
const PANEL_LAYOUT_CONFIG_KEYS: readonly string[] = [
  PANEL_DEV_TREE_COLLAPSED_KEY,
  PANEL_DEV_TOOL_AREA_COLLAPSED_KEY,
  PANEL_DEV_TOOL_AREA_WIDTH_KEY,
];

/** 内容区容器最小形态：工作空间页传 WorkspaceModel、项目任务页传 ProjectModel（结构子集）。 */
export interface WorkbenchContainer {
  id: string;
  name: string;
  dir: string;
  launchSettings?: WorkspaceLaunchSettings;
}

interface WorkbenchContentProps {
  // 解析后的容器（终端 cwd / 启动设置合并基底 / 打开目录 / 归档）：工作空间页传
  // workspace、项目任务页传 project；null = 容器未解析，启动编排闸门等待。
  container: WorkbenchContainer | null;
  // 容器列表解析中（workspace/project 列表请求在途）：launchResolved 闸门消费。
  containerPending: boolean;
  // 当前选中任务对象（工具渲染上下文、启动覆盖合并基底）；null = 未选中或列表加载中。
  task: ProjectTaskResponseData | null;
  // 选中意图（任务 id 已定、对象/容器未必就绪）：空态/加载态/不存在三分支与
  // 工具面板空态文案的判定锚，与 task 对象就绪解耦。
  selectedTaskId: string | null;
  // 任务列表加载中（有选中意图但任务未出时区分 spinner 与「任务不存在」）。
  tasksLoading: boolean;
  // 左树插槽：调用方注入页面各自的树/占位（折叠开合与宽度动画由本组件承担）。
  leftTree: ReactNode;
  // 会话编排语境（T5.3）：'workspace' = 工作空间页（容器即会话归属）；'task' = 项目任务页
  // （选中任务为会话归属，未显式选会话时按 T6.2 编排主会话）；null/缺省 = 页面未接入
  // 会话编排（legacy 行为不变：无会话订阅、无主会话创建、终端捕获/恢复占位恒空）。
  conversationScope?: 'workspace' | 'task' | null;
  // 左树上抛的会话选中意图（会话 id；null = 未选会话，任务语境回落主会话编排）。
  selectedConversationId?: string | null;
  // 选中被清空（归档/删除成功后调用）：调用方负责清 store 选中与 URL 子状态。
  onClearSelection?: () => void;
}

/**
 * WorkbenchContent：工作台内容区（多页共用）。左树插槽｜终端列（标题栏 + 初始化闸门 +
 * 启动分流）+ 工具面板区 + 最右工具条。容器/任务由调用方解析传入，本组件自洽编排
 * 启动时序（配置闸门 → 解析就绪 → TerminalLaunchFlow）与工具 tabs/归档/沉浸模式。
 */
export default function WorkbenchContent({
  container,
  containerPending,
  task,
  selectedTaskId,
  tasksLoading,
  leftTree,
  conversationScope = null,
  selectedConversationId = null,
  onClearSelection,
}: WorkbenchContentProps) {
  const theme = useTheme();
  // 沉浸模式（全屏）：隐藏 App 外壳 + 左树（PanelApp 同步订阅隐藏菜单栏/顶栏）。
  // 会话态存 devWorkbench store；退出路径 = 切换钮 / Esc / 切页卸载复位。
  const workbenchFullscreen = useDevWorkbenchStore(s => s.workbenchFullscreen);
  const setWorkbenchFullscreen = useDevWorkbenchStore(s => s.setWorkbenchFullscreen);
  // 左树折叠态：订阅 config（跨重启持久化、多窗口同步）。
  const treeCollapsed = useConfigValue(PANEL_DEV_TREE_COLLAPSED_KEY, decodeTreeCollapsed, false);
  // 全屏下左树恒隐藏：与用户折叠配置合成（只读不改写 config，退出全屏按原配置恢复）。
  // 该钮在全屏下语义转为「退出全屏」——全屏中点击折叠钮无可见反馈，语义让位更直觉。
  const toggleTreeCollapsed = () => {
    if (workbenchFullscreen) {
      setWorkbenchFullscreen(false);
      return;
    }
    void setAppConfig(PANEL_DEV_TREE_COLLAPSED_KEY, toYesNo(!treeCollapsed));
  };

  // 右侧工具面板区折叠态（工具条顶部方格开关）：同左树范式，config 持久化（跨重启、多窗口同步）。
  const toolAreaCollapsed = useConfigValue(PANEL_DEV_TOOL_AREA_COLLAPSED_KEY, decodeToolAreaCollapsed, true);
  const toggleToolAreaCollapsed = () => {
    void setAppConfig(PANEL_DEV_TOOL_AREA_COLLAPSED_KEY, toYesNo(!toolAreaCollapsed));
  };
  // 工具面板区宽度：拖拽结束落盘（ToolPanelArea up 回调），拖拽期纯组件内存态。
  const toolAreaWidth = useConfigValue(PANEL_DEV_TOOL_AREA_WIDTH_KEY, decodeToolAreaWidth, DEFAULT_PANEL_DEV_TOOL_AREA_WIDTH);
  const commitToolAreaWidth = (width: number) => {
    void setAppConfig(PANEL_DEV_TOOL_AREA_WIDTH_KEY, String(width));
  };

  const initWorkspace = useInitTaskWorkspace();

  // 归档/取消（T3.2）：⋯ 菜单入口 + 两段式确认（首确认 → 后端安全检查 → 警告态强确认）。
  const { show: showToast, snack: toastSnack } = useToast();
  const archiveWorkspace = useArchiveIssueWorkspace();
  const [actionsMenuAnchor, setActionsMenuAnchor] = useState<HTMLElement | null>(null);
  // 确认弹窗态：null = 关闭；warnings = null 首确认态，非空 = 安全检查警告态（二次确认）。
  const [archiveConfirm, setArchiveConfirm] = useState<{ kind: IssueWorkspaceArchiveAction; warnings: string[] | null } | null>(null);

  // —— 会话编排（T5.3/T6.2）：owner 解析 → 列表订阅 → 主会话单飞 ——
  // 归属随页面语境：任务页 = 选中任务（未显式选会话时主会话承载）；工作空间页 = 容器本身。
  const convOwnerType: 'workspace' | 'task' = conversationScope === 'task' ? 'task' : 'workspace';
  const convOwnerId = conversationScope === 'task'
    ? task?.id ?? null
    : conversationScope === 'workspace' ? container?.id ?? null : null;
  const {
    data: conversationRows,
    isPending: conversationsPending,
    isFetching: conversationsFetching,
  } = useConversations(convOwnerType, convOwnerId);
  const mainConversation = useTaskMainConversation(
    conversationScope === 'task' ? container : null,
    conversationScope === 'task' ? task : null,
  );
  // 呈现会话：显式选中优先；任务语境未显式选中 → 主会话编排承载。
  const activeConversation = selectedConversationId != null && conversationRows != null
    ? conversationRows.find(c => c.id === selectedConversationId) ?? null
    : null;

  // 选中门禁（按 conversationScope 收敛）：任务页 = 选中任务；工作空间页 = 选中会话。
  // 工具条/工具面板/全屏等消费方随锚定启用，不再要求任务对象在场。
  const hasSelection = conversationScope === 'workspace' ? selectedConversationId != null : selectedTaskId != null;
  // 工具面板锚定：任务页 = 选中任务；工作空间页 = 选中会话（projectId 回落容器 id，
  // containerId 标记容器归属 + conversationScope 供各工具切数据面；锚定不改变查询语义）。
  const toolIssue: WorkbenchToolIssue | null = conversationScope === 'workspace'
    ? (container != null && selectedConversationId != null
        ? {
            id: selectedConversationId,
            projectId: container.id,
            name: activeConversation?.title ?? container.name,
            containerId: container.id,
            conversationScope: 'workspace',
          }
        : null)
    : task != null
      ? { ...task, conversationScope: 'task' }
      : null;

  // 生效启动设置（T0.4）：task 覆盖字段级合并 container，未覆盖键回落。合并值驱动
  // 终端直启派生与启动分流；task/container/catalog 任一解析中时 startupCli=null
  // （消费端闸门等待，保证 directCommand/Agent 展示名首值即终值——编码规则 1）。
  const effLaunch = mergeLaunchSettings(container?.launchSettings, task?.launchSettings);
  const { data: agentCatalog = [], isPending: agentCatalogPending } = useAgentCatalog();
  const launchResolved = !containerPending && !agentCatalogPending && task != null && container != null;
  // 会话消费面的启动解析（工作空间页无任务对象：容器 + catalog 就绪即可，闸门语义同源）。
  const conversationLaunchResolved = !containerPending && !agentCatalogPending && container != null;
  // 呈现内容是否 ACP 会话视图（选中会话或任务主会话 mode=acp）。
  const acpPresented = activeConversation?.mode === 'acp'
    || (selectedConversationId == null && conversationScope === 'task' && mainConversation.main?.mode === 'acp');
  // 过渡浮层开关：仅配置模式为终端且呈现内容是终端时播放——none 档（含解析中）children
  // 是启动方式选择面板、acp 会话 children 是 ACP 视图，均无「启动终端」语义，浮层叠上成重影。
  const terminalTransitionEnabled = !containerPending && effLaunch?.mode === 'terminal-auto' && !acpPresented;
  const startupCli: string | null = launchResolved
    ? terminalStartupCli(effLaunch?.mode, effLaunch?.autoCommand)
    : null;
  const containerDir = container?.dir ?? null;
  // 打开目录按钮可见性（容器语义）：任务页维持 task 门禁不回归；工作空间页容器目录在场即渲染
  // （点击开容器根目录，与该页终端 cwd 同源），目录缺失则语义不存在、不渲染。
  const showOpenDir = hasSelection && (task != null || (conversationScope === 'workspace' && containerDir != null && containerDir !== ''));

  // 归档成功的本地痕迹清理（工具 tabs + 预览 tabs + 终端分屏布局，按容器 id 前缀隔离的
  // localStorage key，对任意 id 无副作用）。
  const clearTaskLocalTraces = (taskId: string) => {
    clearToolTabs(taskId);
    clearPreviewTabs(taskId);
    removeLayouts(taskId);
  };

  // 提交归档/取消：首确认（warnings=null → force=false，后端干净则内部续发执行段）/
  // 警告态强确认（warnings 非空 → force=true 只走执行段）。成功后按序清理：该任务本地
  // 痕迹（clearTaskLocalTraces）→ 经 onClearSelection 清选中 + 清 URL（调用方双清防恢复）。
  const submitArchive = () => {
    if (archiveConfirm == null || task == null || container == null) {
      return;
    }
    const { kind, warnings } = archiveConfirm;
    archiveWorkspace.mutate(
      { workspaceId: container.id, taskId: task.id, action: kind, force: warnings != null },
      {
        onSuccess: (result) => {
          if (result.status === 'warnings') {
            setArchiveConfirm({ kind, warnings: result.warnings });
            return;
          }
          setArchiveConfirm(null);
          showToast(kind === 'archive' ? '已归档：工作空间目录已删除' : '已取消：工作空间目录已删除', 'success');
          clearTaskLocalTraces(task.id);
          onClearSelection?.();
        },
        onError: (e) => {
          showToast(e instanceof Error ? e.message : String(e), 'error');
        },
      },
    );
  };

  // 工具面板区可见性：仅由用户展开（config）决定，与任务选中态解耦——面板内容虽围绕
  // 选中任务，但选中/取消选中驱动的开合动画是布局闪动来源；未选中时空态文案兜底（见
  // ToolPanelArea，按 hasSelection 二分）。
  const toolAreaVisible = !toolAreaCollapsed;

  // 左树隐藏合成：用户折叠配置 || 沉浸模式（只读不改写 config，退出全屏按原配置恢复）。
  const treeHidden = treeCollapsed || workbenchFullscreen;

  // 布局配置就绪闸门（见 PANEL_LAYOUT_CONFIG_KEYS 注释）：就绪后才渲染布局，首帧即终值。
  const panelConfigReady = useConfigReady(PANEL_LAYOUT_CONFIG_KEYS);

  // ⌘W/Ctrl+W 页面级接管（macOS 应用菜单已让位，见 src/shared/useCloseWindowShortcut.ts
  // 头注释）：文件查看模式（当前任务预览 tabs 非空且有激活 tab）→ 关闭激活 tab（关闭后
  // 激活右侧相邻的规则由 closePreviewTab 纯函数统一，⌘W/Escape/关闭钮同源）；否则保持
  // 默认关窗（走既有 CloseRequested → prevent_close + hide 链）。事件时 getState() 现读，
  // 无陈旧闭包；不建渲染订阅——内容区不因 tabs 变化多一次渲染。
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.key.toLowerCase() !== 'w') {
        return;
      }
      e.preventDefault();
      // 锚定随页语境：工作空间页锚选中会话，任务页锚选中任务（浮层 tabs 同源）。
      const anchorId = conversationScope === 'workspace' ? selectedConversationId : task?.id ?? null;
      const { previewTabsByIssue, closePreviewTab: closeTab } = useWorkspaceFilesStore.getState();
      const tabs = anchorId != null ? previewTabsByIssue[anchorId] : undefined;
      if (anchorId != null && tabs != null && tabs.tabs.length > 0 && tabs.activeTabId != null) {
        closeTab(anchorId, tabs.activeTabId);
      } else {
        void getCurrentWindow().close();
      }
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [conversationScope, selectedConversationId, task?.id]);

  // Esc 直退沉浸模式（全屏态下预览浮层的 Esc 关 tab 让位——浮层 onKeyDown 读 store 判断）。
  // 仅全屏态挂监听；Dialog 类浮层（归档确认/md 代码块全屏）内的 Esc 由 MUI Modal 自身
  // stopPropagation 吞掉。终端内键入的 Esc（claude CLI/vim 等 TUI 高频按键）按事件来源
  // 让位——xterm 的 keydown 只 preventDefault 不 stopPropagation，会冒泡到 document，
  // 若不排除会把终端交互误判为退出全屏（且外壳弹回触发终端宽度骤变的 SIGWINCH 重绘）。
  useEffect(() => {
    if (!workbenchFullscreen) {
      return;
    }
    const handler = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') {
        return;
      }
      if ((e.target as HTMLElement | null)?.closest('.xterm') != null) {
        return;
      }
      e.preventDefault();
      useDevWorkbenchStore.getState().setWorkbenchFullscreen(false);
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [workbenchFullscreen]);

  // 切出工作台（卸载）复位沉浸模式：外壳隐藏是内容区级语义，离开页面必须恢复——
  // 否则下次进入工作台带着隐藏外壳启动，且菜单栏/顶栏（含命令面板入口）不可达。
  useEffect(() => () => {
    useDevWorkbenchStore.getState().setWorkbenchFullscreen(false);
  }, []);

  // 会话体（T5.3）：mode 分流——acp → ACP 会话视图；terminal → 现终端启动链原样（直启/
  // 临场两选一），接线 T2.1 捕获锚归属与 T2.2 锚恢复（锚非空才注入）。pane 锚 =
  // `${容器id}::${会话id}::${paneId}`：容器 id 任务页取 task.id、工作空间页取 workspace.id
  // （每会话独立 pane 集合，切会话互不干扰）；cwd 任务页 = 任务工作区、工作空间页 = 空间根目录。
  const renderConversationContent = (conversation: ConversationModel, taskScoped: boolean): ReactNode => {
    if (conversation.mode === 'acp') {
      return (
        <TerminalErrorBoundary key={conversation.id} title="会话视图异常退出">
          <AcpSessionView conversationId={conversation.id} />
        </TerminalErrorBoundary>
      );
    }
    return (
      <TerminalLaunchFlow
        key={conversation.id}
        containerId={taskScoped && task != null ? task.id : container?.id ?? conversation.id}
        workspaceDir={taskScoped ? containerDir : null}
        cwdOverride={taskScoped ? null : containerDir}
        launchMode={effLaunch?.mode ?? 'none'}
        autoCommand={effLaunch?.autoCommand}
        terminalAgentLabel={terminalAgentLabel(effLaunch?.autoCommand)}
        acpAgentLabel={acpAgentLabel(agentCatalog, effLaunch?.agentCode)}
        conversationId={conversation.id}
        resumeClaudeId={conversation.claudeSessionId !== '' ? conversation.claudeSessionId : null}
      />
    );
  };

  // 终端列内容分支序：显式会话（两页通用）→ 任务（闸门内主会话编排 / legacy 直通）→
  // 任务意图等待 → 空态。异步数据未就绪一律占位等待，禁止「先默认渲染后纠正」（§2.3）。
  let terminalContent: ReactNode;
  if (selectedConversationId != null && convOwnerId != null) {
    if (conversationsPending || (activeConversation == null && conversationsFetching)) {
      terminalContent = <CenterSpinner />;
    } else if (activeConversation == null) {
      terminalContent = <CenterNote text="会话不存在或已删除" />;
    } else if (conversationScope === 'task' && task != null) {
      // 任务语境选中会话：与主会话同受初始化闸门约束（终端 cwd 依赖工作区目录就绪）。
      terminalContent = (
        <WorkspaceInitGate
          key={task.id}
          issueId={task.id}
          startupCli={startupCli}
          terminalTransitionEnabled={terminalTransitionEnabled}
        >
          <TerminalErrorBoundary key={task.id}>
            {conversationLaunchResolved
              ? renderConversationContent(activeConversation, true)
              : <Box sx={{ height: '100%' }} />}
          </TerminalErrorBoundary>
        </WorkspaceInitGate>
      );
    } else {
      // 工作空间语境：无初始化闸门（cwd = 工作空间根目录），启动解析就绪即挂会话体。
      terminalContent = conversationLaunchResolved
        ? renderConversationContent(activeConversation, false)
        : <Box sx={{ height: '100%' }} />;
    }
  } else if (task != null) {
    terminalContent = (
      <WorkspaceInitGate
        key={task.id}
        issueId={task.id}
        startupCli={startupCli}
        // 过渡浮层仅 children 为终端时播放（开关派生见上方 terminalTransitionEnabled）。
        terminalTransitionEnabled={terminalTransitionEnabled}
      >
        <TerminalErrorBoundary key={task.id}>
          {/* 启动配置解析中（task/container/catalog 任一未就绪）不挂载启动分流（空占位
          等待）——挂载即终值，directCommand/启动语义无中途变化路径（编码规则 1）。 */}
          {!launchResolved
            ? <Box sx={{ height: '100%' }} />
            : conversationScope !== 'task'
              ? (
                  // legacy 语境（页面未接入会话编排）：无会话实体上下文，捕获/恢复占位恒空
                  // （锚会话段走占位，形态仍三段）。
                  <TerminalLaunchFlow
                    key={task.id}
                    containerId={task.id}
                    workspaceDir={containerDir}
                    launchMode={effLaunch?.mode ?? 'none'}
                    autoCommand={effLaunch?.autoCommand}
                    terminalAgentLabel={terminalAgentLabel(effLaunch?.autoCommand)}
                    acpAgentLabel={acpAgentLabel(agentCatalog, effLaunch?.agentCode)}
                  />
                )
              : mainConversation.main == null
                ? (mainConversation.error != null
                    ? (
                        <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: '12px', height: '100%', p: '16px' }}>
                          <Typography variant="body2" color="error">主会话获取失败：{mainConversation.error.message}</Typography>
                          <Button size="small" variant="outlined" onClick={mainConversation.retry}>重试</Button>
                        </Box>
                      )
                    : <CenterSpinner />)
                : renderConversationContent(mainConversation.main, true)}
        </TerminalErrorBoundary>
      </WorkspaceInitGate>
    );
  } else if (selectedTaskId != null) {
    terminalContent = tasksLoading
      ? <CenterSpinner />
      : <CenterNote text="任务不存在或已移出开发流程" />;
  } else {
    terminalContent = <CenterNote text={conversationScope === 'workspace' ? '请先选择左侧会话' : '请先选择左侧任务'} />;
  }

  // 折叠态配置未就绪：空占位，避免首帧默认展开 → 动画收起的宽度翻转。
  if (!panelConfigReady) {
    return <Box sx={{ height: '100%' }} />;
  }

  return (
    <Box sx={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
      {/* 左栏：调用方注入的树插槽。
          恒渲染 + width 过渡动画折叠，折叠到 0 后右栏占满整宽。
          沉浸模式（全屏）与用户折叠配置合成隐藏（treeHidden）——不改写 config，
          退出全屏按原配置恢复。 */}
      <Box
        sx={{
          width: treeHidden ? '0px' : '260px',
          flexShrink: 0,
          borderRight: treeHidden ? '0px' : '1px solid',
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
        <Box sx={{ width: '260px', flexShrink: 0, display: 'flex', flexDirection: 'column', height: '100%' }}>
          {leftTree}
        </Box>
      </Box>

      {/* 中栏 = 内容行：终端列（含标题栏）+ 工具面板区（tab 头与标题栏同带对齐）。
          标题栏下沉终端列——操作组（ml:auto）落终端列右缘：面板展开时紧贴其左边界，不被推到最右。 */}
      <Box sx={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* 终端列：标题栏 + 终端内容；minWidth 与工具面板区拖拽上限联动（ToolPanelArea 按容器实测宽收紧 max） */}
        <Box sx={{ flex: 1, minWidth: TERMINAL_MIN_WIDTH, minHeight: '0px', display: 'flex', flexDirection: 'column' }}>
          {/* 标题栏：左树折叠开关 + 选中任务名称 + 右侧快捷区。
              macOS Overlay：沉浸模式下外壳（含顶栏）不渲染、本栏顶到窗口 y=0，带
              data-tauri-drag-region 承担窗口拖拽，pl 让位红绿灯 80px（非沉浸时本栏在
              顶栏 y=44 之下、无重叠，不避让）。名称纯展示非交互，其余空白区域拖拽语义不变。 */}
          <Box
            data-tauri-drag-region={isMacOS || undefined}
            sx={{
              height: WORKBENCH_TITLEBAR_HEIGHT,
              flexShrink: 0,
              display: 'flex',
              alignItems: 'center',
              px: '8px',
              pl: workbenchFullscreen && isMacOS ? `${TRAFFIC_LIGHT_CLEARANCE}px` : '8px',
              gap: '4px',
              borderBottom: '1px solid',
              borderColor: 'divider',
              bgcolor: 'background.paper',
            }}
          >
            <IconButton
              size="small"
              onClick={toggleTreeCollapsed}
              aria-label={workbenchFullscreen ? '退出全屏' : treeCollapsed ? '显示任务列表' : '隐藏任务列表'}
              sx={TITLEBAR_ICON_SX}
            >
              {treeHidden ? <ViewSidebarOutlinedIcon /> : <ViewSidebarIcon />}
            </IconButton>
            {hasSelection && task && (
              <Typography variant="subtitle1" noWrap sx={{ fontWeight: 600 }}>
                {task.name}
              </Typography>
            )}
            <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: '4px', flexShrink: 0 }}>
              {/* 打开工作区目录（胶囊分体按钮，OpenWorkspaceDir/ 特性目录）：左段图标 =
                  当前默认工具（点击即打开），hover/▼ 展开浮层切默认（串行：打开成功后才切）。
                  任务页开任务子目录、工作空间页开容器根目录（issueId null）。 */}
              {showOpenDir && <OpenWorkspaceDirButton issueId={task?.id ?? null} workspaceDir={containerDir} />}
              {/* 清理终端并重新初始化（T1.5）：清理该任务全部终端会话后走增量初始化（已成功步骤/仓库跳过），
                与 WorkspaceInitGate 面板按钮共用同一 mutation/query key，面板自动切换回进度态。 */}
              {hasSelection && task && (
                <Tooltip title="清理终端并重新初始化">
                  <span>
                    <IconButton
                      size="small"
                      aria-label="清理终端并重新初始化"
                      disabled={initWorkspace.isPending}
                      onClick={() => initWorkspace.mutate({ taskId: task.id })}
                      sx={TITLEBAR_ICON_SX}
                    >
                      <CleaningServicesIcon />
                    </IconButton>
                  </span>
                </Tooltip>
              )}
              {/* 沉浸模式（全屏）：隐藏 App 外壳（菜单栏/顶栏）与左树，本区占满窗口；
                  右侧工具面板区保持当前展开/收起态。Esc 直退（浮层 Esc 关 tab 让位）。
                  纯 UI 操作随选中门禁启用，任务维度按钮的 task 条件不适用。 */}
              {hasSelection && (
                <Tooltip title={workbenchFullscreen ? '退出全屏' : '全屏（隐藏菜单栏与任务树）'}>
                  <IconButton
                    size="small"
                    aria-label={workbenchFullscreen ? '退出全屏' : '全屏'}
                    onClick={() => setWorkbenchFullscreen(!workbenchFullscreen)}
                    sx={TITLEBAR_ICON_SX}
                  >
                    {workbenchFullscreen ? <CloseFullscreenIcon /> : <OpenInFullIcon />}
                  </IconButton>
                </Tooltip>
              )}
              {/* 任务操作菜单（T3.2）：⋯ 入口，归档/取消（删工作空间目录 + 流转任务状态，两段式确认） */}
              {hasSelection && task && (
                <>
                  <Tooltip title="更多任务操作">
                    <IconButton
                      size="small"
                      aria-label="更多任务操作"
                      onClick={e => setActionsMenuAnchor(e.currentTarget)}
                      sx={TITLEBAR_ICON_SX}
                    >
                      <MoreHorizIcon />
                    </IconButton>
                  </Tooltip>
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

          {/* 终端内容（分支计算见 terminalContent）：显式会话 → 任务主会话/legacy 直通 →
              任务意图等待 → 空态。relative 供文件预览浮层（absolute inset 0 铺满本区、
              不挤压布局零 SIGWINCH）锚定。 */}
          <Box sx={{ flex: 1, minHeight: '0px', position: 'relative' }}>
            {terminalContent}
            {/* 工作空间文件预览浮层：tabs 按锚定 id 隔离（任务页=任务、工作空间页=会话）、
                非空才可见；与终端内容为兄弟节点，absolute 定位不参与 flex 布局（会话后端
                常驻仅视觉遮盖）。工作空间页 workspaceId 使五查询/fileRaw 走空间目录本身。 */}
            {task != null && <FilePreviewOverlay issueId={task.id} />}
            {conversationScope === 'workspace' && container != null && selectedConversationId != null && (
              <FilePreviewOverlay issueId={selectedConversationId} workspaceId={container.id} />
            )}
            {/* 浏览器操作台浮层：激活 profile 接管终端内容区（FilePreviewOverlay 同款接管
                语义），按锚定命名空间过滤（任务页 = 任务族、工作空间页 = 工作空间族）；
                显隐自派生（未激活渲染 null）。 */}
            {conversationScope === 'workspace'
              ? (
                  container != null && selectedConversationId != null
                  && <BrowserStageOverlay ownerKind="workspace" ownerId={container.id} />
                )
              : task != null && <BrowserStageOverlay ownerKind="issue" ownerId={task.id} />}
          </Box>
        </Box>
        <ToolPanelArea
          issue={toolIssue}
          hasSelection={hasSelection}
          emptyHint={conversationScope === 'workspace' ? '请先选择左侧会话' : '请先选择左侧任务'}
          visible={toolAreaVisible}
          width={toolAreaWidth}
          onWidthCommit={commitToolAreaWidth}
        />
      </Box>

      {/* 最右常驻工具条：顶部方格（面板区总开关）+ 工具图标列（注册表驱动）。未选中时
          仅工具图标禁用（工具须挂具体任务），方格开关保持可用（面板恒展示后须能收起）。 */}
      <WorkbenchToolRail
        issueId={toolIssue?.id ?? null}
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
              ? '取消将删除该任务的工作空间目录（含全部仓库的本地修改与未推送提交）、关闭其终端会话，任务置为「已取消」。'
              : '归档将删除该任务的工作空间目录（含全部仓库的本地修改与未推送提交）、关闭其终端会话，任务置为「已完成」。'}
          </Typography>
          {archiveConfirm?.warnings != null && (
            <Alert severity="warning" sx={{ mt: '12px' }}>
              安全检查发现以下问题，执行后这些内容将丢失：
              <Box component="ul" sx={{ my: '4px', pl: '16px' }}>
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
