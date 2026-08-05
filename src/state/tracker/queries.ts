import type {
  ProjectIssueCreateRequest,
  ProjectIssueUpdateRequest,
  WorkspaceCreateRequest,
  WorkspaceProjectCreateRequest,
  WorkspaceProjectUpdateRequest,
  WorkspaceUpdateRequest,
} from '@src/services';
import {
  ProjectIssueService,
  ProjectStateService,
  WorkspaceProjectService,
  WorkspaceService,
} from '@src/services';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { trackerKeys } from './keys';
import { useTrackerStore } from './store';

// ─── 读取（query）───
// 列表查询：消费方共享同一 queryKey 即共享缓存（命令面板二级页与主页面不会重复请求）。

/** 全部工作空间。 */
export function useWorkspaces() {
  return useQuery({
    queryKey: trackerKeys.workspaces(),
    queryFn: () => WorkspaceService.getList(),
  });
}

/** 指定工作空间下的项目。workspaceId 为 null 时不查询（如命令面板在某工作空间选中前不查项目）。 */
export function useWorkspaceProjects(workspaceId: number | null) {
  return useQuery({
    queryKey: trackerKeys.workspaceProjects(workspaceId ?? 0),
    queryFn: () => WorkspaceProjectService.getList({ workspaceId: workspaceId! }),
    enabled: workspaceId != null,
  });
}

/** 指定项目的 projectIssue（含 labels）。 */
export function useProjectIssues(projectId: number) {
  return useQuery({
    queryKey: trackerKeys.projectIssues(projectId),
    queryFn: () => ProjectIssueService.getList({ projectId }),
  });
}

/** 指定项目的状态列表。 */
export function useProjectStates(projectId: number) {
  return useQuery({
    queryKey: trackerKeys.projectStates(projectId),
    queryFn: () => ProjectStateService.getList({ projectId }),
  });
}

// ─── 写操作（mutation）───
// 每个 mutation 内部 invalidate 对应 key；消费方只调 mutateAsync/mutate，不关心失效。
// workspaceId/projectId 入参仅用于失效对应列表缓存。

/** 创建工作空间。 */
export function useCreateWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: WorkspaceCreateRequest) => WorkspaceService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.workspaces() }),
  });
}

/** 更新工作空间。 */
export function useUpdateWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: WorkspaceUpdateRequest) => WorkspaceService.update(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.workspaces() }),
  });
}

/** 删除工作空间。 */
export function useDeleteWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => WorkspaceService.delete({ id }),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.workspaces() }),
  });
}

/** 创建项目（workspaceId 用于失效该工作空间的项目缓存）。 */
export function useCreateWorkspaceProject(workspaceId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: WorkspaceProjectCreateRequest) => WorkspaceProjectService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.workspaceProjects(workspaceId) }),
  });
}

/** 更新项目。 */
export function useUpdateWorkspaceProject(workspaceId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: WorkspaceProjectUpdateRequest) => WorkspaceProjectService.update(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.workspaceProjects(workspaceId) }),
  });
}

/** 删除项目；若删的正是当前选中项目，联动清空 store 选中态（替代原 WorkspaceProjectList onSelect(null) 副作用）。 */
export function useDeleteWorkspaceProject(workspaceId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => WorkspaceProjectService.delete({ id }),
    onSuccess: (_data, deletedId) => {
      qc.invalidateQueries({ queryKey: trackerKeys.workspaceProjects(workspaceId) });
      const cur = useTrackerStore.getState().selectedWorkspaceProject;
      if (cur?.id === deletedId) {
        useTrackerStore.getState().selectWorkspaceProject(null);
      }
    },
  });
}

// ─── Issue 写操作（mutation）───
// 每个 mutation 内部 invalidate 对应 key；消费方只调 mutateAsync/mutate，不关心失效。
// 注：看板拖拽的乐观更新另走 ProjectIssueList.updateProjectIssues（setQueryData），不经过这些 mutation。

/** 创建 projectIssue。 */
export function useCreateProjectIssue(projectId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ProjectIssueCreateRequest) => ProjectIssueService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.projectIssues(projectId) }),
  });
}

/** 更新 projectIssue。 */
export function useUpdateProjectIssue(projectId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ProjectIssueUpdateRequest) => ProjectIssueService.update(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.projectIssues(projectId) }),
  });
}

/** 删除 projectIssue。 */
export function useDeleteProjectIssue(projectId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => ProjectIssueService.delete({ id }),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.projectIssues(projectId) }),
  });
}
