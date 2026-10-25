/**
 * projects 域对外 API（唯一入口）。
 * 消费方只从此处 import hooks/keys，域内部 queries 重构不波及消费方。
 */
export { projectsKeys } from './keys';
export {
  useCreateProject,
  useDeleteProject,
  useProjects,
  useUpdateProject,
} from './queries';
