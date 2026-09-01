import type { IssueWorkspaceStatusResponseData } from '@src/services';
import { IssueWorkspaceService } from '@src/services';
import { commands } from '@src/shared/bindings';
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
