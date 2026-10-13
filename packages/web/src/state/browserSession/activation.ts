import { create } from 'zustand';

/**
 * 浏览器操作台激活态（client 选中态，zustand）：激活 profile 接管工作台终端内容区
 * 浮层（BrowserStageOverlay），跨 BrowserPanel（激活入口）与 DevWorkbenchPage（浮层
 * 挂载点）两个组件树共享。切 issue 不重置——激活的是全局单源的 profile 会话，浮层
 * 跟随 profile 存续。
 */
interface BrowserStageState {
  /** 激活到操作台浮层的 profile（null = 未激活，终端内容区正常显示）。 */
  activatedProfile: string | null;
  /** 各 profile 的观看页码（null/缺省 = 当前页；页面行点选 = 分析目标与观看页码同语义）。 */
  watchPage: Record<string, number | null>;
  activate: (profile: string) => void;
  deactivate: () => void;
  /** 页面行点选：同码再点取消（回落当前页）。 */
  toggleWatchPage: (profile: string, pageIndex: number) => void;
}

export const useBrowserStageStore = create<BrowserStageState>()(set => ({
  activatedProfile: null,
  watchPage: {},
  activate: profile => set({ activatedProfile: profile }),
  deactivate: () => set({ activatedProfile: null }),
  toggleWatchPage: (profile, pageIndex) =>
    set(state => ({
      watchPage: { ...state.watchPage, [profile]: state.watchPage[profile] === pageIndex ? null : pageIndex },
    })),
}));
