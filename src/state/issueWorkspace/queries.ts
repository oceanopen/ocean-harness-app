import type { IssueWorkspaceArchiveAction, IssueWorkspaceStatusResponseData } from '@src/services';
import { IssueWorkspaceService } from '@src/services';
import { commands } from '@src/shared/bindings';
import { trackerKeys } from '@src/state/tracker';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { issueWorkspaceKeys } from './keys';

// ─── 读取（query）───

/**
 * 工作空间初始化状态（issueId 维度缓存）。baseDir 未设置（空串）时不查询——Go 侧要求
 * 绝对路径，先由 UI 引导设置。RUNNING 时 1s 轮询（全库首个 refetchInterval 用例），
 * 终态/未初始化自动停（返回 false），切 issue 换 key 天然重建。
 */
export function useIssueWorkspaceStatus(issueId: string | null, baseDir: string) {
  return useQuery({
    queryKey: issueWorkspaceKeys.status(issueId ?? ''),
    queryFn: () => IssueWorkspaceService.status({ issueId: issueId!, baseDir }),
    enabled: issueId != null && baseDir !== '',
    refetchInterval: query => (query.state.data?.serverStatus === 'RUNNING' ? 1000 : false),
  });
}

// ─── 写操作（mutation）───

/**
 * 触发（重新）初始化：先清理该 issue 的全部 PTY 会话（重新初始化要求终端对象重建，
 * 会话可能本就不存在——清理失败仅告警不阻断），再调 init（幂等：已完成步骤/仓库跳过）。
 * 返回的受理态直接 setQueryData 就地更新缓存（同 localRepositories 的 refresh 先例）。
 */
export function useInitIssueWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (req: { issueId: string; baseDir: string }) => {
      const shutdown = await commands.ptyShutdownIssue(req.issueId);
      if (shutdown.status === 'error') {
        console.warn('[issueWorkspace] ptyShutdownIssue failed (ignore, session may not exist):', shutdown.error);
      }
      return IssueWorkspaceService.init(req);
    },
    onSuccess: (data, req) => {
      qc.setQueryData<IssueWorkspaceStatusResponseData>(issueWorkspaceKeys.status(req.issueId), data);
    },
  });
}

/** useArchiveIssueWorkspace 的入参（projectId 用于成功后失效 issue 列表缓存；force 仅用于警告二次确认后的执行段）。 */
export interface ArchiveIssueWorkspaceArgs {
  projectId: number;
  issueId: string;
  baseDir: string;
  action: IssueWorkspaceArchiveAction;
  force?: boolean;
}

/** 归档/取消结果：warnings = 检查发现未提交/未推送，待二次确认；done = 已执行（目录已删、状态已流转）。 */
export type ArchiveIssueWorkspaceResult
  = | { status: 'warnings'; warnings: string[] }
    | { status: 'done' };

/**
 * 归档/取消工作空间（T3.2）。force 缺省走两段式：先调检查（force=false），干净则内部
 * 续发执行段、有警告返回 {status:'warnings'} 由 UI 弹警告态，二次确认后携带 force 重发
 * （此时只走执行段）。执行段固定时序：ptyShutdownIssue（删目录前必杀该 issue 全部终端
 * 会话，失败仅告警不阻断——会话可能本就不存在）→ archive(force=true)（删目录 + 后端
 * 流转状态）。成功后失效双缓存：issue 列表（左树/顶栏/子任务面板）与工作空间状态
 * （回 NOT_INITIALIZED）。
 */
export function useArchiveIssueWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (args: ArchiveIssueWorkspaceArgs): Promise<ArchiveIssueWorkspaceResult> => {
      const { projectId: _projectId, ...req } = args;
      if (!args.force) {
        const check = await IssueWorkspaceService.archive({ ...req, force: false });
        if (check.warnings.length > 0) {
          return { status: 'warnings', warnings: check.warnings };
        }
      }
      const shutdown = await commands.ptyShutdownIssue(args.issueId);
      if (shutdown.status === 'error') {
        console.warn('[issueWorkspace] ptyShutdownIssue failed (ignore, session may not exist):', shutdown.error);
      }
      await IssueWorkspaceService.archive({ ...req, force: true });
      return { status: 'done' };
    },
    onSuccess: (result, args) => {
      if (result.status !== 'done') {
        return;
      }
      qc.invalidateQueries({ queryKey: trackerKeys.projectIssues(args.projectId) });
      qc.invalidateQueries({ queryKey: issueWorkspaceKeys.status(args.issueId) });
    },
  });
}
