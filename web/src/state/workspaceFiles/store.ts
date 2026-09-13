// workspaceFiles 域 client 状态：预览浮层 tabs（按 issue 隔离 + localStorage 持久化）与
// 文件树展开目录集（按 issue 隔离、会话级不持久化）。
//
// 预览 tabs 持久化：与 toolTabs/terminalPanes 同心智（「一 issue 一工作环境」）——切 issue
// 往返/重启后预览现场还原；文件内容不入持久化（query 缓存重取，绝不陈旧）。
// 树展开态不持久化但入域 store：文件面板在 ToolPanelArea 中非激活即卸载，组件 local state
// 会丢展开现场；域内存态是唯一不引入新范式的存放位。
//
// store 形状：单 record（issueId → 成对视图），订阅/写回/持久化三处成对（克隆
// workbenchTools/store.ts 骨架，含 hydration + subscribe 落盘 + useXxx 成对封装 hook）。

import type { PreviewTabsState } from './actions';
import { create } from 'zustand';
import {
  closePreviewTab,
  EMPTY_PREVIEW_TABS,
  loadPreviewTabs,
  openPreviewTab,
  savePreviewTabs,
  setActivePreviewTab,
} from './actions';

/// 默认展开目录集的种子：repo/（工作空间仓库根——issue 工作空间布局 {issueId}/repo/<name>/，
/// 展开它即直见各仓库名）。仅作「用户从未 toggle 过」时的渲染期回落与首次 toggle 的基底，
/// 不随持久化流转。
export const DEFAULT_EXPANDED_DIR = 'repo';

function seedDefaultExpanded(): Set<string> {
  return new Set([DEFAULT_EXPANDED_DIR]);
}

interface WorkspaceFilesState {
  /// issueId → 预览 tabs + 激活态成对视图（持久化，见 actions.ts）。
  previewTabsByIssue: Record<string, PreviewTabsState>;
  /// issueId → 展开目录 path 集（会话级，不持久化）。undefined = 该 issue 未 toggle 过
  /// （渲染层回落 DEFAULT_EXPANDED_DIR）。
  expandedDirsByIssue: Record<string, Set<string>>;
  openPreviewTab: (issueId: string, path: string) => void;
  closePreviewTab: (issueId: string, path: string) => void;
  closeAllPreviewTabs: (issueId: string) => void;
  setActivePreviewTab: (issueId: string, path: string) => void;
  toggleDirExpanded: (issueId: string, dirPath: string) => void;
}

export const useWorkspaceFilesStore = create<WorkspaceFilesState>()(set => ({
  previewTabsByIssue: {},
  expandedDirsByIssue: {},
  openPreviewTab: (issueId, path) => set((state) => {
    const prev = state.previewTabsByIssue[issueId] ?? EMPTY_PREVIEW_TABS;
    const next = openPreviewTab(prev, path);
    return next === prev ? state : { previewTabsByIssue: { ...state.previewTabsByIssue, [issueId]: next } };
  }),
  closePreviewTab: (issueId, path) => set((state) => {
    const prev = state.previewTabsByIssue[issueId] ?? EMPTY_PREVIEW_TABS;
    const next = closePreviewTab(prev, path);
    return next === prev ? state : { previewTabsByIssue: { ...state.previewTabsByIssue, [issueId]: next } };
  }),
  // 关闭全部（tab 栏右缘一键关闭）：置共享空常量（引用稳定），落盘链自动移除 localStorage key，
  // 浮层随空 tabs 卸载。已是空态时原样返回（免无效重渲染）。
  closeAllPreviewTabs: issueId => set((state) => {
    const prev = state.previewTabsByIssue[issueId];
    if (prev == null || prev === EMPTY_PREVIEW_TABS) {
      return state;
    }
    return { previewTabsByIssue: { ...state.previewTabsByIssue, [issueId]: EMPTY_PREVIEW_TABS } };
  }),
  setActivePreviewTab: (issueId, path) => set((state) => {
    const prev = state.previewTabsByIssue[issueId] ?? EMPTY_PREVIEW_TABS;
    const next = setActivePreviewTab(prev, path);
    return next === prev ? state : { previewTabsByIssue: { ...state.previewTabsByIssue, [issueId]: next } };
  }),
  // 展开态基底 = store 记录 ?? 默认集（渲染期派生同款基底）——首次 toggle 前用户看到的就是
  // repo/ 展开，toggle 从该基底出发，「先默认后纠正」不发生（set 内同步计算，无二次渲染）。
  toggleDirExpanded: (issueId, dirPath) => set((state) => {
    const current = state.expandedDirsByIssue[issueId] ?? seedDefaultExpanded();
    const next = new Set(current);
    if (next.has(dirPath)) {
      next.delete(dirPath);
    } else {
      next.add(dirPath);
    }
    return { expandedDirsByIssue: { ...state.expandedDirsByIssue, [issueId]: next } };
  }),
}));

/// hydration：store 无该 issue 记录时从 localStorage 读回写入（渲染期调用，同步）。
/// 幂等——已有记录（本会话操作过）原样保留。返回 store 持有的引用（水合失败落
/// EMPTY_PREVIEW_TABS 共享常量）。只负责 hydration 副作用，响应式读取另走 selector
/// （裸调 getState 不建立订阅，workbenchTools.ensureTabs 同款契约）。
export function ensurePreviewTabs(issueId: string): PreviewTabsState {
  const existing = useWorkspaceFilesStore.getState().previewTabsByIssue[issueId];
  if (existing != null) {
    return existing;
  }
  const restored = loadPreviewTabs(issueId);
  useWorkspaceFilesStore.setState(s => ({
    previewTabsByIssue: { ...s.previewTabsByIssue, [issueId]: restored },
  }));
  return restored;
}

// 持久化订阅：单 record 引用变化 → 逐 issue 比较落盘（仅变更项；空 tabs 移除 key）。
// 模块级一次性注册（多窗口各自 JS realm 独立，无重复订阅问题）。expandedDirs 不落盘。
let lastPersisted: Record<string, PreviewTabsState> = {};
useWorkspaceFilesStore.subscribe((state) => {
  for (const [issueId, tabs] of Object.entries(state.previewTabsByIssue)) {
    if (lastPersisted[issueId] !== tabs) {
      savePreviewTabs(issueId, tabs);
    }
  }
  lastPersisted = state.previewTabsByIssue;
});

/// 消费方统一订阅入口：hydration（幂等裸调）+ 响应式订阅 + 空位回落 EMPTY_PREVIEW_TABS
/// 成对封装（useToolTabs 同款契约）。issueId 为 null（未选中）恒返回 EMPTY_PREVIEW_TABS。
export function usePreviewTabs(issueId: string | null): PreviewTabsState {
  if (issueId != null) {
    ensurePreviewTabs(issueId);
  }
  return useWorkspaceFilesStore(s => (issueId != null ? s.previewTabsByIssue[issueId] : undefined)) ?? EMPTY_PREVIEW_TABS;
}

/// 展开目录集订阅：undefined = 该 issue 未 toggle 过（消费方回落默认集 DEFAULT_EXPANDED_DIR）。
export function useExpandedDirs(issueId: string | null): ReadonlySet<string> | undefined {
  return useWorkspaceFilesStore(s => (issueId != null ? s.expandedDirsByIssue[issueId] : undefined));
}
