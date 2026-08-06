// tracker 域对外 API（唯一入口）。
// 消费方只从此处 import hooks/keys，域内部 store/queries 重构不波及消费方。
export { trackerKeys } from './keys';
export {
  buildStateViews,
  useCreateProjectIssue,
  useCreateWorkspace,
  useCreateWorkspaceProject,
  useDeleteProjectIssue,
  useDeleteWorkspace,
  useDeleteWorkspaceProject,
  useProjectIssues,
  useProjectStates,
  useProjectStateViews,
  useStateCatalog,
  useUpdateProjectIssue,
  useUpdateWorkspace,
  useUpdateWorkspaceProject,
  useWorkspaceProjects,
  useWorkspaces,
} from './queries';
export type { ProjectStateView } from './queries';
export { useTrackerStore } from './store';
