// workbenchTools 域纯函数与按 issue 持久化（参照 terminalPanes 范式）。
// 工具 tab 模型：互斥工具（子任务/文件目录）同类单 tab（tab id = toolId，单例语义）；
// 并存工具（浏览器）每实例一个 tab（tab id = randomUUID 前 8 位，跨重启不撞）。
// tabs/activeTab 按 issue 隔离（一 issue 一工作环境，与终端布局同心智）：
// localStorage 按 issue 存 JSON，损坏/缺失回落空 tabs——工具面板均为纯展示派生，
// 丢失仅 UI 回落重新手动打开，无副作用。

/// 工具 tab：id 为 tab 标识（互斥工具 = toolId；并存工具 = 随机短 id），toolId 指向注册表项。
/// 渲染层以注册表查 def，查不到的 toolId（未来下线的工具遗留 tab）过滤不渲染。
export interface ToolTab {
  id: string;
  toolId: string;
}

/// 打开工具的结果（tabs + 激活 tab 成对变更）。
export interface ToolTabsState {
  tabs: ToolTab[];
  activeTabId: string | null;
}

/// 空 tabs 视图（域级共享常量，对齐 terminalPanes 的 INITIAL_LAYOUT）：store 空位回落、
/// 读回判废回落、组件 selector fallback 共用同一引用——保证 getSnapshot 引用稳定。
export const EMPTY_TOOL_TABS: ToolTabsState = { tabs: [], activeTabId: null };

/// 并存工具新实例 tab id：randomUUID 前 8 位（对齐 terminalPanes newPaneId 设计）。
export function newToolTabId(): string {
  return crypto.randomUUID().slice(0, 8);
}

/// 打开工具（rail 图标点击 / 未来 tab 栏 + 号）。互斥（exclusive=true）：同类 tab 已存在则
/// 激活之（已是激活态则原样返回——重复点击无操作）；不存在则追加并激活。并存：总是追加
/// 新实例并激活。面板区展开由调用方另行驱动（config SSOT），本函数只管 tabs。
export function openToolTabs(state: ToolTabsState, toolId: string, exclusive: boolean): ToolTabsState {
  if (exclusive) {
    const existing = state.tabs.find(t => t.toolId === toolId);
    if (existing != null) {
      return existing.id === state.activeTabId ? state : { tabs: state.tabs, activeTabId: existing.id };
    }
    const tab: ToolTab = { id: toolId, toolId };
    return { tabs: [...state.tabs, tab], activeTabId: tab.id };
  }
  const tab: ToolTab = { id: newToolTabId(), toolId };
  return { tabs: [...state.tabs, tab], activeTabId: tab.id };
}

/// 关闭 tab：移除后若关的是激活 tab 则激活相邻（优先左侧兄弟，无则右侧，清空为 null——
/// 面板区保持展开不联动收起，空 tab 栏由后续 + 号下拉补充）。tabId 不存在时原样返回。
export function closeToolTab(state: ToolTabsState, tabId: string): ToolTabsState {
  const index = state.tabs.findIndex(t => t.id === tabId);
  if (index < 0) {
    return state;
  }
  const tabs = state.tabs.filter(t => t.id !== tabId);
  if (state.activeTabId !== tabId) {
    return { tabs, activeTabId: state.activeTabId };
  }
  const neighbor = tabs[Math.max(0, index - 1)] ?? null;
  return { tabs, activeTabId: neighbor?.id ?? null };
}

/// 激活 tab（tab 头点击）：仅改激活态，tabs 不动；已是激活态原样返回（引用不变免重渲染）。
export function setActiveToolTab(state: ToolTabsState, tabId: string): ToolTabsState {
  return state.activeTabId === tabId ? state : { tabs: state.tabs, activeTabId: tabId };
}

// ---------- 持久化（localStorage 按 issue，参照 terminalPanes） ----------

/// localStorage key：workbench_tool_tabs_<issueId>（对齐 terminal_pane_layout_ 命名）。
function tabsKey(issueId: string): string {
  return `workbench_tool_tabs_${issueId}`;
}

/// 结构校验：防手改/版本演进/截断产生的脏数据进渲染层。规则：tabs 为数组且每项
/// id/toolId 均非空字符串、tab id 无重复；activeTabId 为 null 或在 tabs 内。任何一处
/// 不满足 → 整体判废（回落空 tabs）。未知 toolId 不在此拦截（注册表在组件层，渲染时过滤）。
function isValidTabsState(parsed: unknown): parsed is ToolTabsState {
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
    if (typeof t.id !== 'string' || t.id.length === 0 || typeof t.toolId !== 'string' || t.toolId.length === 0) {
      return false;
    }
    if (seen.has(t.id)) {
      return false;
    }
    seen.add(t.id);
  }
  return s.activeTabId == null || (typeof s.activeTabId === 'string' && seen.has(s.activeTabId));
}

/// 读回 issue 的工具 tabs：无记录/JSON 解析失败/结构校验失败 → EMPTY_TOOL_TABS（共享引用）。
export function loadToolTabs(issueId: string): ToolTabsState {
  try {
    const raw = localStorage.getItem(tabsKey(issueId));
    if (raw == null) {
      return EMPTY_TOOL_TABS;
    }
    const parsed: unknown = JSON.parse(raw);
    return isValidTabsState(parsed) ? parsed : EMPTY_TOOL_TABS;
  } catch {
    // JSON.parse 抛错（截断/非法）等同判废，不区分。
    return EMPTY_TOOL_TABS;
  }
}

/// 写入 issue 的工具 tabs：空 tabs（且无激活）移除 key——localStorage 不积 corpse，
/// 也保证「关闭全部 tab」后下次打开回到干净空面板。
export function saveToolTabs(issueId: string, state: ToolTabsState): void {
  try {
    if (state.tabs.length === 0 && state.activeTabId == null) {
      localStorage.removeItem(tabsKey(issueId));
      return;
    }
    localStorage.setItem(tabsKey(issueId), JSON.stringify(state));
  } catch (e) {
    // 写失败（隐私模式/超限）不致命：tabs 仅 UI 状态，下次会话回落空面板。
    console.warn('[workbenchTools] save tabs failed:', e);
  }
}

/// 清理 issue 的工具 tabs 记录（归档/取消 onSuccess 调用——issue 级本地痕迹随任务终结移除）。
export function clearToolTabs(issueId: string): void {
  try {
    localStorage.removeItem(tabsKey(issueId));
  } catch (e) {
    console.warn('[workbenchTools] clear tabs failed:', e);
  }
}
