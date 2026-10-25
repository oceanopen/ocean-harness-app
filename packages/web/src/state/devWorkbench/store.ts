import { create } from 'zustand';

/**
 * devWorkbench 域 client 状态：工作台沉浸模式开关。不启用 persist：会话态不跨重启、不跨窗口；
 * 左树/工具面板区折叠态不在此处——走 appConfig 持久化（PANEL_DEV_TREE_COLLAPSED_KEY 等）。
 */
interface DevWorkbenchUiState {
  /**
   * 工作台沉浸模式（全屏）：隐藏 App 外壳（PanelApp 左侧菜单栏 + 顶部导航栏）与工作台左树，
   * 标题栏 + 内容区 + 右侧工具面板区占满窗口；入口在标题栏切换钮，退出 = 再点 / Esc 直退
   * （全屏态下预览浮层的 Esc 关 tab 让位）/ 切出工作台（卸载复位）。跨 store 域消费（PanelApp
   * 订阅隐藏外壳），故入全局 store 而非组件局部 state。
   */
  workbenchFullscreen: boolean;
  setWorkbenchFullscreen: (v: boolean) => void;
}

export const useDevWorkbenchStore = create<DevWorkbenchUiState>()(set => ({
  workbenchFullscreen: false,
  setWorkbenchFullscreen: v => set({ workbenchFullscreen: v }),
}));
