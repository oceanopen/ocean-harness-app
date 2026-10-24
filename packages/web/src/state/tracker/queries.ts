import type {
  ProjectTaskCreateRequest,
  ProjectTaskUpdateRequest,
  ProjectTypeCreateRequest,
  ProjectTypeUpdateRequest,
  WorkspaceCreateRequest,
  WorkspaceUpdateRequest,
} from '@src/services';
import { ProjectTaskService, ProjectTypeService, WorkspaceService } from '@src/services';
import { commands } from '@src/shared/bindings';
import { logOnError } from '@src/shared/commands';
import { imBotsKeys } from '@src/state/imBots/keys';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { trackerKeys } from './keys';

// ─── 读取（query）───
/**
 * 列表查询：消费方共享同一 queryKey 即共享缓存（命令面板二级页与主页面不会重复请求）；
 * 无状态相关 query。
 */

/** 全部工作空间。 */
export function useWorkspaces() {
  return useQuery({
    queryKey: trackerKeys.workspaces(),
    queryFn: () => WorkspaceService.getList(),
  });
}

/** 指定项目下的全部类型。projectId 为 null/空串时不查询（桥接期无项目归属）。 */
export function useProjectTypes(projectId: string | null) {
  return useQuery({
    queryKey: trackerKeys.projectTypes(projectId ?? ''),
    queryFn: () => ProjectTypeService.getList({ projectId: projectId! }),
    enabled: projectId != null && projectId !== '',
  });
}

/** 指定工作空间下的 workspaceIssue（含类型）。workspaceId 为 null/空串时不查询（无效请求守卫）。 */
export function useProjectTasks(projectId: string | null) {
  return useQuery({
    queryKey: trackerKeys.projectTasks(projectId ?? ''),
    queryFn: () => ProjectTaskService.getList({ projectId: projectId! }),
    enabled: projectId != null && projectId !== '',
  });
}

// ─── 写操作（mutation）───
/**
 * 每个 mutation 内部 invalidate 对应 key；消费方只调 mutateAsync/mutate，不关心失效。
 * workspaceId 入参仅用于失效对应列表缓存。
 */

/** 创建工作空间。 */
export function useCreateWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: WorkspaceCreateRequest) => WorkspaceService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.workspaces() }),
  });
}

/** 更新工作空间。同步失效 imBots 列表（bot 卡片展示空间名，且目录变更影响其运行）。 */
export function useUpdateWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: WorkspaceUpdateRequest) => WorkspaceService.update(req),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: trackerKeys.workspaces() });
      void qc.invalidateQueries({ queryKey: imBotsKeys.list() });
    },
  });
}

/** 删除工作空间。同步失效 imBots 列表（后端置空关联 bot 的 workspace_id）。 */
export function useDeleteWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    // 删除前关掉该工作空间名下全部终端 pane（会话维度锚首段=workspace.id，前缀扫描覆盖）。
    mutationFn: async (id: string) => {
      await logOnError(commands.ptyShutdownIssue(id), 'pty');
      return WorkspaceService.delete({ id });
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: trackerKeys.workspaces() });
      void qc.invalidateQueries({ queryKey: imBotsKeys.list() });
    },
  });
}

// ─── 类型写操作（mutation）───
/**
 * workspaceId 入参用于失效该工作空间的类型缓存（issue 抽屉与管理抽屉共享同一份列表）。
 */

/** 创建类型（projectId 用于失效该项目的类型缓存）。 */
export function useCreateProjectType(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ProjectTypeCreateRequest) => ProjectTypeService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.projectTypes(projectId) }),
  });
}

/** 更新类型。 */
export function useUpdateProjectType(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ProjectTypeUpdateRequest) => ProjectTypeService.update(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.projectTypes(projectId) }),
  });
}

/** 删除类型（后端将引用该类型的任务置为未分类）。 */
export function useDeleteProjectType(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => ProjectTypeService.delete({ id }),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.projectTypes(projectId) }),
  });
}

// ─── Issue 写操作（mutation）───
/**
 * 每个 mutation 内部 invalidate 对应 key；消费方只调 mutateAsync/mutate，不关心失效。
 */

/** 创建任务。 */
export function useCreateProjectTask(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ProjectTaskCreateRequest) => ProjectTaskService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.projectTasks(projectId) }),
  });
}

/** 更新任务。 */
export function useUpdateProjectTask(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ProjectTaskUpdateRequest) => ProjectTaskService.update(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.projectTasks(projectId) }),
  });
}

/**
 * 删除任务。先关其全部终端 pane（任务 id 即 pane 锚首段 `${taskId}::${会话id}::${paneId}`，
 * 前缀扫描全关该任务全部会话的全部 pane；不关则孤儿会话占 store 至 app 退出）。
 */
export function useDeleteProjectTask(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await logOnError(commands.ptyShutdownIssue(id), 'pty');
      return ProjectTaskService.delete({ id });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.projectTasks(projectId) }),
  });
}
