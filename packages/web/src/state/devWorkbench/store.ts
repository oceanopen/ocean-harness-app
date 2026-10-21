import type { WorkspaceIssueResponseData } from '@src/services';
import { create } from 'zustand';

/**
 * devWorkbench 域 client 状态：左任务树的 issue 级选中态（workspace 级选择复用 tracker store）。
 * 不启用 persist：选中态仅会话内有效（与 tracker 一致，重启重新初始化）；左栏折叠态不在此处——走 appConfig 持久化（PANEL_DEV_TREE_COLLAPSED_KEY，见 DevWorkbenchPage）。
 */
interface DevWorkbenchSelectionState {
  selectedIssueId: string | null; // 当前选中的任务 issue id（uuid 字符串）
  selectIssue: (issue: WorkspaceIssueResponseData | null) => void;
  /**
   * 工作台沉浸模式（全屏）：隐藏 App 外壳（PanelApp 左侧菜单栏 + 顶部导航栏）与工作台左栏任务树，issue 标题栏 + 内容区 + 右侧工具面板区占满窗口；右侧工具面板区保持用户当前展开/收起态不变。
   * 入口在 issue 标题栏「...」左侧切换钮；退出 = 再点 / Esc 直退（全屏态下预览浮层的 Esc 关 tab 让位）/ 切出 devWorkbench 页面（卸载复位）。
   * 会话态不持久化：跨 store 域消费（PanelApp 订阅隐藏外壳），故入全局 store 而非页面局部 state；不走 appConfig——不跨重启、不跨窗口同步。
   */
  workbenchFullscreen: boolean;
  setWorkbenchFullscreen: (v: boolean) => void;
}

export const useDevWorkbenchStore = create<DevWorkbenchSelectionState>()(set => ({
  selectedIssueId: null,
  selectIssue: issue => set({ selectedIssueId: issue?.id ?? null }),
  workbenchFullscreen: false,
  setWorkbenchFullscreen: v => set({ workbenchFullscreen: v }),
}));
