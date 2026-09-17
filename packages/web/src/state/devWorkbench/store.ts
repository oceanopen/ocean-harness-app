import type { ProjectIssueResponseData } from '@src/services';
import { create } from 'zustand';

// devWorkbench 域 client 状态：左任务树的 issue 级选中态。
// 存 projectId 供右栏按 project 查询 issue 详情（实时派生，避免 store 快照陈旧——issue 变更后 query 自动刷新）。
// workspace/project 级选择复用 tracker store（issue 数据挂 workspace→project，共享最省事）。
// 不启用 persist：选中态仅会话内有效（与 tracker 一致，重启重新初始化）。
// 左栏折叠态不在此处——走 appConfig 持久化（PANEL_DEV_TREE_COLLAPSED_KEY，见 DevWorkbenchPage）。
interface DevWorkbenchSelectionState {
  // 当前选中的开发任务 issue id（uuid 字符串）+ 其所属 project id（右栏据此查询 issue 详情）。
  selectedIssueId: string | null;
  selectedProjectId: number | null;
  selectIssue: (issue: ProjectIssueResponseData | null) => void;
  // 润色意图（T3.3）：TrackerPage 抽屉「AI 润色」按钮写入 {issueId, requestedAt}，
  // 跳转工作台后由终端注入编排（EmbeddedTerminal/useRefineInjection）消费。一次性
  // 语义，以下任一即清：claude 已运行提示手动 / 注入完成 / 就绪超时（claude 未
  // 起来）/ 意图过期（编排启动前搁置超 10 分钟——闸门卡在未初始化等场景视为用户
  // 已放弃，防数小时后进入开发时误注入）。不持久化——刷新/重启视为放弃意图，
  // 绝不因页面恢复误补注入。
  pendingRefine: { issueId: string; requestedAt: number } | null;
  requestRefine: (issueId: string) => void;
  clearRefine: () => void;
  // 工作台沉浸模式（全屏）：隐藏 App 外壳（PanelApp 左侧菜单栏 + 顶部导航栏）与工作台
  // 左栏任务树，issue 标题栏 + 内容区 + 右侧工具面板区占满窗口；右侧工具面板区保持
  // 用户当前展开/收起态不变。入口在 issue 标题栏「...」左侧切换钮；退出 = 再点 / Esc
  // 直退（全屏态下预览浮层的 Esc 关 tab 让位）/ 切出 devWorkbench 页面（卸载复位）。
  // 会话态不持久化：跨 store 域消费（PanelApp 订阅隐藏外壳），故入全局 store 而非页面
  // 局部 state；不走 appConfig——不跨重启、不跨窗口同步。
  workbenchFullscreen: boolean;
  setWorkbenchFullscreen: (v: boolean) => void;
}

export const useDevWorkbenchStore = create<DevWorkbenchSelectionState>()(set => ({
  selectedIssueId: null,
  selectedProjectId: null,
  selectIssue: issue => set({
    selectedIssueId: issue?.id ?? null,
    selectedProjectId: issue?.projectId ?? null,
  }),
  pendingRefine: null,
  requestRefine: issueId => set({ pendingRefine: { issueId, requestedAt: Date.now() } }),
  clearRefine: () => set({ pendingRefine: null }),
  workbenchFullscreen: false,
  setWorkbenchFullscreen: v => set({ workbenchFullscreen: v }),
}));
