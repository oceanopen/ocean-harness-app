import type {
  BrowserSessionAnalyzeRequest,
  BrowserSessionCloseRequest,
  BrowserSessionEnsureRequest,
  BrowserSessionNavigateRequest,
  BrowserSessionSnapshot,
} from '@src/services';
import { BrowserSessionService } from '@src/services';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { browserSessionKeys } from './keys';
import { upsertSessions } from './reducer';

/** 全量会话投影快照读端：与 SSE 首帧/增量共用同一缓存，推送驱动更新，本查询不依赖 refetch。 */
export function useBrowserSessionView() {
  return useQuery({
    queryKey: browserSessionKeys.view,
    queryFn: () => BrowserSessionService.getInfo(),
  });
}

/**
 * 拉起/复用 profile 会话（同步等就绪，D10 headless 覆写）：受理即返回行——按 profile
 * upsert 落缓存（缓存为空以单行快照起步），starting → ready 迁移经 SSE sessions 帧跟进。
 */
export function useEnsureBrowserSession() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: BrowserSessionEnsureRequest) => BrowserSessionService.ensure(req),
    onSuccess: (row) => {
      queryClient.setQueryData<BrowserSessionSnapshot>(browserSessionKeys.view, prev =>
        upsertSessions(prev ?? { sessions: [] }, [row]));
    },
  });
}

/**
 * 释放会话（幂等，无头/有头切换 = close + ensure 传新值的前半程）：响应 = 释放后的投影
 * 行（收尾前服务端已捕获引擎真实当前页，切换恢复现场的权威源）；缓存由 SSE sessions 帧
 * 驱动回写，本地不动。
 */
export function useCloseBrowserSession() {
  return useMutation({
    mutationFn: (req: BrowserSessionCloseRequest) => BrowserSessionService.close(req),
  });
}

/** 当前页打开地址（引擎结果透传；页面列表与状态刷新全经 SSE，本地不动缓存）。 */
export function useNavigateBrowserSession() {
  return useMutation({
    mutationFn: (req: BrowserSessionNavigateRequest) => BrowserSessionService.navigate(req),
  });
}

/** 「AI 分析此页」即时受理（回答走 issue 绑定 agent 会话既有链路，本面板只发起）。 */
export function useAnalyzeBrowserSession() {
  return useMutation({
    mutationFn: (req: BrowserSessionAnalyzeRequest) => BrowserSessionService.analyze(req),
  });
}

/**
 * 截图视口轮询（伪实时帧源）：enabled 数据条件驱动——会话 ready 且面板可见才轮询
 * （会话释放/面板收起即停；轮询本身是 idle 保活，D4「用户在看就不释放」的展示侧对偶），
 * 1.5s 一帧。截图调用在后端照记 recentCalls（豁免会丢失败可见性），「最近操作」展示层
 * 过滤成功行（见 BrowserSessionCard）。
 */
export function useBrowserScreenshot(profile: string, enabled: boolean) {
  return useQuery({
    queryKey: browserSessionKeys.screenshot(profile),
    queryFn: () => BrowserSessionService.screenshot({ profile }),
    enabled,
    refetchInterval: 1500,
  });
}
