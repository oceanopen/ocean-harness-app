// workbenchTools 域 client 状态：开发工作台右侧工具面板区的 tab 集合与激活态（按 issue 隔离）。
//
// 纯前端 UI 状态——工具面板内容均为查询派生（子任务列表复用 tracker 缓存），tabs 丢失的
// 最坏情况 = 回落空面板重新手动打开，无副作用。
//
// store 形状：单 record（issueId → ToolTabsState 成对视图）——tabs 与 activeTabId 在纯函数/
// 持久化/消费方三处均成对出现，拆双 record 会引入取视图/写回/订阅三份粘合代码（terminalPanes
// 的 layouts 同为单 record）。消费方响应式订阅 `s.tabsByIssue[issueId] ?? EMPTY_TOOL_TABS`
// 引用稳定（zustand v5 getSnapshot 缓存契约）。
//
// 持久化：localStorage 按 issue（loadToolTabs/saveToolTabs）。hydration 由消费方渲染期
// 调 ensureTabs（store 无记录时同步写入读回值，首帧即还原——先 hydration 后 selector，
// 同帧读到水合值）；模块级 subscribe 监听单 record 引用变化逐 issue 落盘。面板区可见性
// 与宽度不在此处——走 appConfig 持久化（PANEL_DEV_TOOL_AREA_COLLAPSED/WIDTH_KEY）。
//
// exclusive 语义由调用方传入（注册表在组件层 toolRegistry，状态域不依赖 UI）。

import type { ToolTabsState } from './actions';
import { create } from 'zustand';
import {
  closeToolTab,
  EMPTY_TOOL_TABS,
  loadToolTabs,
  openToolTabs,
  saveToolTabs,
  setActiveToolTab,
} from './actions';

interface WorkbenchToolsState {
  /// issueId → tabs + 激活态成对视图。未登记 issue 惰性回落 EMPTY_TOOL_TABS（不预填）；
  /// 打开过的 issue 经 ensureTabs hydration 后有记录（持久化还原）。
  tabsByIssue: Record<string, ToolTabsState>;
  openTool: (issueId: string, toolId: string, exclusive: boolean) => void;
  closeTab: (issueId: string, tabId: string) => void;
  setActiveTab: (issueId: string, tabId: string) => void;
}

export const useWorkbenchToolsStore = create<WorkbenchToolsState>()(set => ({
  tabsByIssue: {},
  openTool: (issueId, toolId, exclusive) => set((state) => {
    const prev = state.tabsByIssue[issueId] ?? EMPTY_TOOL_TABS;
    const next = openToolTabs(prev, toolId, exclusive);
    return next === prev ? state : { tabsByIssue: { ...state.tabsByIssue, [issueId]: next } };
  }),
  closeTab: (issueId, tabId) => set((state) => {
    const prev = state.tabsByIssue[issueId] ?? EMPTY_TOOL_TABS;
    const next = closeToolTab(prev, tabId);
    return next === prev ? state : { tabsByIssue: { ...state.tabsByIssue, [issueId]: next } };
  }),
  setActiveTab: (issueId, tabId) => set((state) => {
    const prev = state.tabsByIssue[issueId] ?? EMPTY_TOOL_TABS;
    const next = setActiveToolTab(prev, tabId);
    return next === prev ? state : { tabsByIssue: { ...state.tabsByIssue, [issueId]: next } };
  }),
}));

/// hydration：store 无该 issue 记录时从 localStorage 读回写入（渲染期调用，同步）。
/// 幂等——已有记录（本会话操作过）原样保留。返回 store 持有的引用（水合失败落
/// EMPTY_TOOL_TABS 共享常量），消费方 selector 引用稳定契约依赖此约定。
/// 注意：本函数只负责 hydration 副作用——响应式读取必须另走 useWorkbenchToolsStore
/// selector（裸调 getState 不建立订阅，参照 terminalPanes：ensureLayout 管水合、
/// TerminalPaneRoot selector 管响应）。
export function ensureTabs(issueId: string): ToolTabsState {
  const existing = useWorkbenchToolsStore.getState().tabsByIssue[issueId];
  if (existing != null) {
    return existing;
  }
  const restored = loadToolTabs(issueId);
  useWorkbenchToolsStore.setState(s => ({
    tabsByIssue: { ...s.tabsByIssue, [issueId]: restored },
  }));
  return restored;
}

// 持久化订阅：单 record 引用变化 → 逐 issue 比较落盘（仅变更项；空 tabs 移除 key）。
// 模块级一次性注册（多窗口各自 JS realm 独立，无重复订阅问题）。
let lastPersisted: Record<string, ToolTabsState> = {};
useWorkbenchToolsStore.subscribe((state) => {
  for (const [issueId, tabs] of Object.entries(state.tabsByIssue)) {
    if (lastPersisted[issueId] !== tabs) {
      saveToolTabs(issueId, tabs);
    }
  }
  lastPersisted = state.tabsByIssue;
});

/// 消费方统一订阅入口：hydration（幂等裸调）+ 响应式订阅 + 空位回落 EMPTY_TOOL_TABS
/// 成对封装。范式自带陷阱——裸调 ensureTabs 不建立订阅，漏配 selector 时开/关/切 tab
/// 不重渲染（terminalPanes 同构：ensureLayout 管水合、TerminalPaneRoot selector 管响应；
/// 彼处仅一个消费方无需封装，本域多消费方统一走此 hook 防范式复制走样）。
/// issueId 为 null（未选中）恒返回 EMPTY_TOOL_TABS。
export function useToolTabs(issueId: string | null): ToolTabsState {
  if (issueId != null) {
    ensureTabs(issueId);
  }
  return useWorkbenchToolsStore(s => (issueId != null ? s.tabsByIssue[issueId] : undefined)) ?? EMPTY_TOOL_TABS;
}
