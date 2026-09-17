// workbenchTools 域对外 API（唯一入口）。消费方只从此处 import。
export type { ToolTab, ToolTabsState } from './actions';
export { clearToolTabs, EMPTY_TOOL_TABS, loadToolTabs, saveToolTabs } from './actions';
export { ensureTabs, useToolTabs, useWorkbenchToolsStore } from './store';
