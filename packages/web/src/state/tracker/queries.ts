import type {
  WorkspaceCreateRequest,
  WorkspaceIssueCreateRequest,
  WorkspaceIssueUpdateRequest,
  WorkspaceTypeCreateRequest,
  WorkspaceTypeUpdateRequest,
  WorkspaceUpdateRequest,
} from '@src/services';
import { WorkspaceIssueService, WorkspaceService, WorkspaceTypeService } from '@src/services';
import { commands } from '@src/shared/bindings';
import { logOnError } from '@src/shared/commands';
import { imBotsKeys } from '@src/state/imBots/keys';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { trackerKeys } from './keys';

// ─── 读取（query）───
/**
 * 列表查询：消费方共享同一 queryKey 即共享缓存（命令面板二级页与主页面不会重复请求）；
 * 状态为双端固定常量（stateMeta.ts），无状态相关 query。
 */

/** 全部工作空间。 */
export function useWorkspaces() {
  return useQuery({
    queryKey: trackerKeys.workspaces(),
    queryFn: () => WorkspaceService.getList(),
  });
}

/** 指定工作空间下的全部类型。workspaceId 为 null 时不查询。 */
export function useWorkspaceTypes(workspaceId: string | null) {
  return useQuery({
    queryKey: trackerKeys.workspaceTypes(workspaceId ?? ''),
    queryFn: () => WorkspaceTypeService.getList({ workspaceId: workspaceId! }),
    enabled: workspaceId != null,
  });
}

/** 指定工作空间下的 workspaceIssue（含类型）。workspaceId 为 null/空串时不查询（无效请求守卫）。 */
export function useWorkspaceIssues(workspaceId: string | null) {
  return useQuery({
    queryKey: trackerKeys.workspaceIssues(workspaceId ?? ''),
    queryFn: () => WorkspaceIssueService.getList({ workspaceId: workspaceId! }),
    enabled: workspaceId != null && workspaceId !== '',
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
    mutationFn: (id: string) => WorkspaceService.delete({ id }),
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

/** 创建类型（workspaceId 用于失效该工作空间的类型缓存）。 */
export function useCreateWorkspaceType(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: WorkspaceTypeCreateRequest) => WorkspaceTypeService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.workspaceTypes(workspaceId) }),
  });
}

/** 更新类型。 */
export function useUpdateWorkspaceType(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: WorkspaceTypeUpdateRequest) => WorkspaceTypeService.update(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.workspaceTypes(workspaceId) }),
  });
}

/** 删除类型（后端将引用该类型的 issue 置为未分类）。 */
export function useDeleteWorkspaceType(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => WorkspaceTypeService.delete({ id }),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.workspaceTypes(workspaceId) }),
  });
}

// ─── Issue 写操作（mutation）───
/**
 * 每个 mutation 内部 invalidate 对应 key；消费方只调 mutateAsync/mutate，不关心失效。
 * 注：看板拖拽的乐观更新另走 WorkspaceIssueList.updateWorkspaceIssues（setQueryData），不经过这些 mutation。
 */

/** 创建 workspaceIssue。 */
export function useCreateWorkspaceIssue(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: WorkspaceIssueCreateRequest) => WorkspaceIssueService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.workspaceIssues(workspaceId) }),
  });
}

/** 更新 workspaceIssue。 */
export function useUpdateWorkspaceIssue(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: WorkspaceIssueUpdateRequest) => WorkspaceIssueService.update(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.workspaceIssues(workspaceId) }),
  });
}

/**
 * 删除 workspaceIssue。先关其全部终端 pane（issue 即 PTY 锚点，split 后一 issue 多 pane
 * 需前缀全关；不关则孤儿会话占 store 至 app 退出）。
 */
export function useDeleteWorkspaceIssue(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await logOnError(commands.ptyShutdownIssue(id), 'pty');
      return WorkspaceIssueService.delete({ id });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: trackerKeys.workspaceIssues(workspaceId) }),
  });
}
