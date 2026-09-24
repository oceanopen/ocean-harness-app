// workspaceFiles 域纯函数与按 issue 持久化（参照 workbenchTools/actions.ts 范式）。
// 预览 tab 模型：tabs 为文件相对路径数组（getFileTree 的 node.path，issue 内天然唯一即
// tab id）——同文件重复点击 = 激活既有 tab（文件面板「已开 tab 行高亮」与浮层 tab 去重
// 共用该语义）。tab 内容形态（文件内容 / 未提交变更 diff）不是 tab 的固有属性，而是渲染
// 期派生值（面板模式 + 变更集动态决定，见 PreviewContent）——一文件一 tab，切模式自动
// 换视图。不需要 workbenchTools 的随机 id（那是多实例并存工具才需要的），也不需要单字段
// 包装对象（path 即身份，包装层无承载）。
// tabs 按 issue 隔离（一 issue 一工作环境）：localStorage 按 issue 存 JSON，损坏/缺失回落
// 空 tabs——预览是纯查看意图，丢失仅 UI 回落重新点开，无副作用。

/// 预览 tabs 成对视图（tabs + activeTabId，纯函数/持久化/消费方三处成对出现，不拆双 record）。
export interface PreviewTabsState {
  tabs: string[];
  activeTabId: string | null;
}

/// 空 tabs 视图（域级共享常量，对齐 workbenchTools 的 EMPTY_TOOL_TABS）：store 空位回落、
/// 读回判废回落、组件 selector fallback 共用同一引用——保证 getSnapshot 引用稳定。
export const EMPTY_PREVIEW_TABS: PreviewTabsState = { tabs: [], activeTabId: null };

/// 打开预览 tab（文件面板点击文件行，两模式统一——内容形态由渲染期派生）：已存在则仅
/// 激活（已是激活态原样返回——重复点击无操作）；不存在则追加并激活。浮层显隐由 tabs
/// 非空派生（无独立开关）。
export function openPreviewTab(state: PreviewTabsState, path: string): PreviewTabsState {
  if (state.tabs.includes(path)) {
    return state.activeTabId === path ? state : { tabs: state.tabs, activeTabId: path };
  }
  return { tabs: [...state.tabs, path], activeTabId: path };
}

/// 关闭 tab：移除后若关的是激活 tab 则激活相邻（优先右侧相邻，无则左侧，清空为 null——
/// 浮层随空 tabs 卸载）。path 不存在时原样返回。⌘W / Escape / tab 关闭钮共用本规则。
export function closePreviewTab(state: PreviewTabsState, path: string): PreviewTabsState {
  const index = state.tabs.indexOf(path);
  if (index < 0) {
    return state;
  }
  const tabs = state.tabs.filter(t => t !== path);
  if (state.activeTabId !== path) {
    return { tabs, activeTabId: state.activeTabId };
  }
  // 关闭位置右侧的 tab 过滤后仍落原 index；关的是末尾 tab 时取其左侧（index - 1）。
  const neighbor = tabs[index] ?? tabs[index - 1] ?? null;
  return { tabs, activeTabId: neighbor };
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

/// 持久化 entry 的原样形态：当前为 string（tab id）；历史模型曾为 `{ path, kind? }` 对象
/// （kind 已废弃），读回时兼容解包。
function entryPathOf(entry: unknown): string | null {
  if (typeof entry === 'string') {
    return entry.length > 0 ? entry : null;
  }
  if (typeof entry === 'object' && entry != null) {
    const p = (entry as Record<string, unknown>).path;
    if (typeof p === 'string' && p.length > 0) {
      return p;
    }
  }
  return null;
}

/// 剥除旧模型（diff tab）的 `diff:` id 前缀，还原真实文件路径。
function stripDiffPrefix(path: string): string {
  return path.startsWith('diff:') ? path.slice('diff:'.length) : path;
}

/// 结构校验：防手改/版本演进/截断产生的脏数据进渲染层。规则：tabs 为数组且每项可解出
/// 非空 path（string 或历史 { path } 对象）、无重复（解包后）；activeTabId 为 null 或在
/// 解包 path 集内。任何一处不满足 → 整体判废（回落空 tabs）。
function isValidTabsState(parsed: unknown): parsed is { tabs: unknown[]; activeTabId: unknown } {
  if (typeof parsed !== 'object' || parsed == null) {
    return false;
  }
  const s = parsed as Record<string, unknown>;
  if (!Array.isArray(s.tabs)) {
    return false;
  }
  const seen = new Set<string>();
  for (const entry of s.tabs) {
    const path = entryPathOf(entry);
    if (path == null || seen.has(path)) {
      return false;
    }
    seen.add(path);
  }
  return s.activeTabId == null || (typeof s.activeTabId === 'string' && seen.has(s.activeTabId));
}

/// 读回 issue 的预览 tabs：无记录/JSON 解析失败/结构校验失败 → EMPTY_PREVIEW_TABS（共享引用）。
/// 存量迁移（两代旧模型）：entry 为 `{ path, kind? }` 对象的解包取 path；diff tab id 的
/// `diff:` 前缀剥除后与同路径 tab 合并去重（保序取首个）。activeTabId 同步剥前缀，若因
/// 合并失效则取迁移后 tabs 的最后一个（最近打开位）。
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
    const tabs: string[] = [];
    const seen = new Set<string>();
    for (const entry of parsed.tabs) {
      const path = stripDiffPrefix(entryPathOf(entry)!);
      if (seen.has(path)) {
        continue;
      }
      seen.add(path);
      tabs.push(path);
    }
    if (tabs.length === 0) {
      return EMPTY_PREVIEW_TABS;
    }
    const activeRaw = parsed.activeTabId as string | null;
    const active = activeRaw == null ? null : stripDiffPrefix(activeRaw);
    return { tabs, activeTabId: seen.has(active ?? '') ? active : tabs[tabs.length - 1] };
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
