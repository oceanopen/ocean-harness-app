// workspaceFiles 域对外 API（唯一入口）。消费方只从此处 import。
export type { PreviewTab, PreviewTabsState } from './actions';
export { clearPreviewTabs, EMPTY_PREVIEW_TABS } from './actions';
export { workspaceFilesKeys } from './keys';
export { useWorkspaceFileContent, useWorkspaceFileTree } from './queries';
export { DEFAULT_EXPANDED_DIR, ensurePreviewTabs, useExpandedDirs, usePreviewTabs, useWorkspaceFilesStore } from './store';
