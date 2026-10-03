import type { AcpSessionCancelRequest, AcpSessionEnsureRequest, AcpSessionPromptRequest, AcpViewSnapshot } from '@src/services';
import { AcpSessionService } from '@src/services';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { acpSessionKeys } from './keys';

/** 会话视图快照读端：与 SSE 首帧/增量共用同一缓存，推送驱动更新，本查询不依赖 refetch。 */
export function useAcpSessionView(issueId: string) {
  return useQuery({
    queryKey: acpSessionKeys.view(issueId),
    queryFn: () => AcpSessionService.getInfo({ issueId }),
  });
}

/**
 * 幂等受理会话创建：受理即返回受理时刻快照——仅在缓存为空时落地（避免覆盖 SSE 已送达
 * 的更新快照），后续状态迁移（starting → ready/failed）经 SSE 跟进。
 */
export function useEnsureAcpSession() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: AcpSessionEnsureRequest) => AcpSessionService.ensure(req),
    onSuccess: (snapshot, req) => {
      queryClient.setQueryData<AcpViewSnapshot>(acpSessionKeys.view(req.issueId), prev => prev ?? snapshot);
    },
  });
}

/** 受理一轮回合（立即返回；回合过程与终态全经 SSE，缓存由帧归约驱动）。 */
export function usePromptAcpSession() {
  return useMutation({
    mutationFn: (req: AcpSessionPromptRequest) => AcpSessionService.prompt(req),
  });
}

/** 软取消当前回合（幂等；终态经 SSE turnEnded 呈现）。 */
export function useCancelAcpSession() {
  return useMutation({
    mutationFn: (req: AcpSessionCancelRequest) => AcpSessionService.cancel(req),
  });
}
