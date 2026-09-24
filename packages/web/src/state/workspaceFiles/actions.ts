// workspaceFiles 域纯函数与按 issue 持久化（参照 workbenchTools/actions.ts 范式）。
// 预览 tab 模型：tab id = 文件相对路径（getFileTree 的 node.path，issue 内天然唯一）——
// 同文件重复点击 = 激活既有 tab（文件面板「已开 tab 行高亮」与浮层 tab 去重共用该语义）；
// diff tab（Git 变更模式）id 带 `diff:` 前缀，与同路径的 file tab 天然隔离。
// 不需要 workbenchTools 的随机 id（那是多实例并存工具才需要的）。
// tabs 按 issue 隔离（一 issue 一工作环境）：localStorage 按 issue 存 JSON，损坏/缺失回落
// 空 tabs——预览是纯查看意图，丢失仅 UI 回落重新点开，无副作用。

/// 预览 tab kind：file=普通文件内容预览；diff=未提交变更 diff 预览（Git 变更模式点开）。
export type PreviewTabKind = 'file' | 'diff';

/// diff tab 的 id 前缀：同一路径的 file tab 与 diff tab 天然去重隔离（互不顶替）。
export const DIFF_TAB_ID_PREFIX = 'diff:';

/// 由 kind 与文件路径派生 tab id（file=路径本身；diff=`diff:`+路径）。
export function previewTabIdOf(kind: PreviewTabKind, path: string): string {
  return kind === 'diff' ? DIFF_TAB_ID_PREFIX + path : path;
}

/// 预览 tab：path 为 tab id（file=相对 {baseDir}/{issueId}/ 的文件路径；diff=`diff:`+路径）；
/// kind 决定内容分派（content query vs fileDiff query）与展示路径的还原。
export interface PreviewTab {
  path: string;
  kind: PreviewTabKind;
}

/// 还原 tab 的真实文件路径（去掉 diff 前缀；title/basename/query 入参共用）。
export function tabFilePath(tab: PreviewTab): string {
  return tab.kind === 'diff' ? tab.path.slice(DIFF_TAB_ID_PREFIX.length) : tab.path;
}

/// 预览 tabs 成对视图（tabs + activeTabId，纯函数/持久化/消费方三处成对出现，不拆双 record）。
export interface PreviewTabsState {
  tabs: PreviewTab[];
  activeTabId: string | null;
}

/// 空 tabs 视图（域级共享常量，对齐 workbenchTools 的 EMPTY_TOOL_TABS）：store 空位回落、
/// 读回判废回落、组件 selector fallback 共用同一引用——保证 getSnapshot 引用稳定。
export const EMPTY_PREVIEW_TABS: PreviewTabsState = { tabs: [], activeTabId: null };

/// 打开预览 tab（文件面板点击文件行；kind 默认 file，Git 变更模式传 diff）：已存在则仅
/// 激活（已是激活态原样返回——重复点击无操作）；不存在则追加并激活。浮层显隐由 tabs
/// 非空派生（无独立开关）。同一路径的 file/diff tab 经 id 前缀天然隔离。
export function openPreviewTab(state: PreviewTabsState, path: string, kind: PreviewTabKind = 'file'): PreviewTabsState {
  const id = previewTabIdOf(kind, path);
  const existing = state.tabs.find(t => t.path === id);
  if (existing != null) {
    return state.activeTabId === id ? state : { tabs: state.tabs, activeTabId: id };
  }
  return { tabs: [...state.tabs, { path: id, kind }], activeTabId: id };
}

/// 关闭 tab：移除后若关的是激活 tab 则激活相邻（优先右侧相邻，无则左侧，清空为 null——
/// 浮层随空 tabs 卸载）。path 不存在时原样返回。⌘W / Escape / tab 关闭钮共用本规则。
export function closePreviewTab(state: PreviewTabsState, path: string): PreviewTabsState {
  const index = state.tabs.findIndex(t => t.path === path);
  if (index < 0) {
    return state;
  }
  const tabs = state.tabs.filter(t => t.path !== path);
  if (state.activeTabId !== path) {
    return { tabs, activeTabId: state.activeTabId };
  }
  // 关闭位置右侧的 tab 过滤后仍落原 index；关的是末尾 tab 时取其左侧（index - 1）。
  const neighbor = tabs[index] ?? tabs[index - 1] ?? null;
  return { tabs, activeTabId: neighbor?.path ?? null };
}

/// 激活 tab（浮层 tab 头点击）：仅改激活态，tabs 不动；已是激活态原样返回（引用不变免重渲染）。
export function setActivePreviewTab(state: PreviewTabsState, path: string): PreviewTabsState {
  return state.activeTabId === path ? state : { tabs: state.tabs, activeTabId: path };
}

// ---------- 持久化（localStorage 按 issue，参照 workbenchTools） ----------

/// localStorage key：workbench_preview_tabs_<issueId>。
function tabsKey(issueId: string): string {
  return `workbench_preview_tabs_${issueId}`;
}

/// 结构校验：防手改/版本演进/截断产生的脏数据进渲染层。规则：tabs 为数组且每项 path
/// 非空、无重复；activeTabId 为 null 或在 tabs 内。任何一处不满足 → 整体判废（回落空 tabs）。
/// kind 为存量兼容字段（diff 模式引入前无此字段）：缺失/非法值按 'file' 处理，不判废。
function isValidTabsState(parsed: unknown): parsed is PreviewTabsState {
  if (typeof parsed !== 'object' || parsed == null) {
    return false;
  }
  const s = parsed as Record<string, unknown>;
  if (!Array.isArray(s.tabs)) {
    return false;
  }
  const seen = new Set<string>();
  for (const tab of s.tabs) {
    if (typeof tab !== 'object' || tab == null) {
      return false;
    }
    const t = tab as Record<string, unknown>;
    if (typeof t.path !== 'string' || t.path.length === 0 || seen.has(t.path)) {
      return false;
    }
    seen.add(t.path);
  }
  return s.activeTabId == null || (typeof s.activeTabId === 'string' && seen.has(s.activeTabId));
}

/// 读回 issue 的预览 tabs：无记录/JSON 解析失败/结构校验失败 → EMPTY_PREVIEW_TABS（共享引用）。
/// kind 归一化：存量记录（diff 模式引入前）无 kind 字段，读回时统一补 'file'。
export function loadPreviewTabs(issueId: string): PreviewTabsState {
  try {
    const raw = localStorage.getItem(tabsKey(issueId));
    if (raw == null) {
      return EMPTY_PREVIEW_TABS;
    }
    const parsed: unknown = JSON.parse(raw);
    if (!isValidTabsState(parsed)) {
      return EMPTY_PREVIEW_TABS;
    }
    return {
      tabs: parsed.tabs.map(t => ({ path: t.path, kind: t.kind === 'diff' ? 'diff' as const : 'file' as const })),
      activeTabId: parsed.activeTabId,
    };
  } catch {
    // JSON.parse 抛错（截断/非法）等同判废，不区分。
    return EMPTY_PREVIEW_TABS;
  }
}

/// 写入 issue 的预览 tabs：空 tabs（且无激活）移除 key——localStorage 不积 corpse，
/// 也保证「关闭全部预览」后下次打开回到干净空浮层。
export function savePreviewTabs(issueId: string, state: PreviewTabsState): void {
  try {
    if (state.tabs.length === 0 && state.activeTabId == null) {
      localStorage.removeItem(tabsKey(issueId));
      return;
    }
    localStorage.setItem(tabsKey(issueId), JSON.stringify(state));
  } catch (e) {
    // 写失败（隐私模式/超限）不致命：预览仅 UI 状态，下次会话回落空浮层。
    console.warn('[workspaceFiles] save preview tabs failed:', e);
  }
}

/// 清理 issue 的预览 tabs 记录（归档/取消 onSuccess 调用——issue 级本地痕迹随任务终结移除）。
export function clearPreviewTabs(issueId: string): void {
  try {
    localStorage.removeItem(tabsKey(issueId));
  } catch (e) {
    console.warn('[workspaceFiles] clear preview tabs failed:', e);
  }
}
