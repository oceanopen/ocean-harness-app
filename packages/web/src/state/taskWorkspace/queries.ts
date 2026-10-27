import type { IssueWorkspaceArchiveAction, TaskWorkspaceStatusResponseData } from '@src/services';
import { TaskWorkspaceService } from '@src/services';
import { commands } from '@src/shared/bindings';
import { trackerKeys } from '@src/state/tracker';
import { workspaceFilesKeys } from '@src/state/workspaceFiles';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { taskWorkspaceKeys } from './keys';

// ─── 读取（query）───

/**
 * 工作空间初始化状态（taskId 维度缓存）。执行中态（受理 PENDING / 后台 RUNNING）1s 轮询
 * 至终态自动停（返回 false）——受理快照先于后台 RUNNING 落盘，仅认 RUNNING 会在 PENDING
 * 窗口停摆冻结；切 issue 换 key 天然重建。
 */
export function useTaskWorkspaceStatus(taskId: string | null) {
  return useQuery({
    queryKey: taskWorkspaceKeys.status(taskId ?? ''),
    queryFn: () => TaskWorkspaceService.status({ taskId: taskId! }),
    enabled: taskId != null,
    refetchInterval: (query) => {
      const serverStatus = query.state.data?.serverStatus;
      return serverStatus === 'PENDING' || serverStatus === 'RUNNING' ? 1000 : false;
    },
  });
}

// ─── 写操作（mutation）───

/**
 * 触发（重新）初始化：先清理该 issue 的全部 PTY 会话（会话可能本就不存在——
 * 清理失败仅告警不阻断），再调 init（幂等：已完成步骤/仓库跳过）。
 * 返回的受理态直接 setQueryData 就地更新缓存；文件树/内容整域失效。
 */
export function useInitTaskWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (req: { taskId: string }) => {
      const shutdown = await commands.ptyShutdownIssue(req.taskId);
      if (shutdown.status === 'error') {
        console.warn('[issueWorkspace] ptyShutdownIssue failed (ignore, session may not exist):', shutdown.error);
      }
      return TaskWorkspaceService.init(req);
    },
    onSuccess: (data, req) => {
      qc.setQueryData<TaskWorkspaceStatusResponseData>(taskWorkspaceKeys.status(req.taskId), data);
      qc.invalidateQueries({ queryKey: workspaceFilesKeys.root });
    },
  });
}

/** useArchiveIssueWorkspace 的入参（workspaceId 用于成功后失效 issue 列表缓存；force 仅用于警告二次确认后的执行段）。 */
export interface ArchiveIssueWorkspaceArgs {
  workspaceId: string; // uuid 字符串
  taskId: string;
  action: IssueWorkspaceArchiveAction;
  force?: boolean;
}

/** 归档/取消结果：warnings = 检查发现未提交/未推送，待二次确认；done = 已执行（目录已删、状态已流转）。 */
export type ArchiveIssueWorkspaceResult
  = | { status: 'warnings'; warnings: string[] }
    | { status: 'done' };

/**
 * 归档/取消任务工作空间（T3.2）。force 缺省走两段式：先调检查（force=false），干净则内部续发执行段、有警告返回 {status:'warnings'} 由 UI 弹警告态，二次确认后携带 force 重发（此时只走执行段）。
 * 执行段固定时序：ptyShutdownIssue（删目录前必杀该 issue 全部终端会话，失败仅告警不阻断——会话可能本就不存在）→ archive(force=true)（删目录 + 后端流转状态）。
 * 成功后失效双缓存：issue 列表（左树/任务管理页）与工作空间状态（回 NOT_INITIALIZED）。
 */
export function useArchiveIssueWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (args: ArchiveIssueWorkspaceArgs): Promise<ArchiveIssueWorkspaceResult> => {
      const { workspaceId: _workspaceId, ...req } = args;
      if (!args.force) {
        const check = await TaskWorkspaceService.archive({ ...req, force: false });
        if (check.warnings.length > 0) {
          return { status: 'warnings', warnings: check.warnings };
        }
      }
      const shutdown = await commands.ptyShutdownIssue(args.taskId);
      if (shutdown.status === 'error') {
        console.warn('[issueWorkspace] ptyShutdownIssue failed (ignore, session may not exist):', shutdown.error);
      }
      await TaskWorkspaceService.archive({ ...req, force: true });
      return { status: 'done' };
    },
    onSuccess: (result, args) => {
      if (result.status !== 'done') {
        return;
      }
      qc.invalidateQueries({ queryKey: trackerKeys.projectTasks(args.workspaceId) });
      qc.invalidateQueries({ queryKey: taskWorkspaceKeys.status(args.taskId) });
    },
  });
}
