import type {
  ImBotBindIssueRequest,
  ImBotCreateRequest,
  ImBotModel,
  ImBotProvisionCancelRequest,
  ImBotProvisionPollRequest,
  ImBotUpdateRequest,
} from '@src/services';
import { ImBotService } from '@src/services';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { imBotsKeys } from './keys';

// ─── 读取（query）───
// 手动刷新模型：操作后由 mutation invalidate 自动刷新，页面提供手动刷新入口。
// 唯一例外是 connecting 瞬态的临时轮询（见 useImBots）——restart 后 connecting →
// connected 迁移无推送通道，不轮询则「连接中」徽标滞留。

/** 全部 IM bot（含运行态投影）。 */
export function useImBots() {
  return useQuery({
    queryKey: imBotsKeys.list(),
    queryFn: () => ImBotService.getList(),
    // 瞬态回填：存在 connecting 态 bot 时临时轮询（数据条件驱动，非定时补发），全部进入
    // 稳定态后自动停；窗口失焦不轮询（refetchIntervalInBackground 默认 false）。
    refetchInterval: query =>
      query.state.data?.some(bot => bot.connState === 'connecting') ? 2000 : false,
  });
}

// ─── 会话域（T3.1 会话级绑定面，T3.2 工具栏面板消费）───

/** 指定 bot 的会话列表（绑定锚 + 活跃时间，最近活跃在前）。 */
export function useImBotConversations(botId: number) {
  return useQuery({
    queryKey: imBotsKeys.conversations(botId),
    queryFn: () => ImBotService.getConversations({ botId }),
    // 无效 bot 行 id（面板过滤/竞态窗口）不发请求。
    enabled: botId > 0,
  });
}

// ─── 写操作（mutation）───
// 每个 mutation 内部 invalidate 整域（列表即本域唯一 server 态）；消费方只调 mutate。

/** list 缓存整体替换：以 mutation 返回的最新实体就地更新（一次往返完成刷新）。 */
function useReplaceInList() {
  const qc = useQueryClient();
  return (updated: ImBotModel) => {
    qc.setQueryData<ImBotModel[]>(imBotsKeys.list(), prev =>
      prev ? prev.map(b => (b.id === updated.id ? updated : b)) : prev);
  };
}

/** 创建 IM bot（启用即拉起连接）。 */
export function useCreateImBot() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ImBotCreateRequest) => ImBotService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: imBotsKeys.list() }),
  });
}

/** 更新 IM bot（secret 留空沿用原值；配置变更即热更新连接）。 */
export function useUpdateImBot() {
  const replace = useReplaceInList();
  return useMutation({
    mutationFn: (req: ImBotUpdateRequest) => ImBotService.update(req),
    onSuccess: updated => replace(updated),
  });
}

/** 删除 IM bot（物理删除 + 停连接）。 */
export function useDeleteImBot() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: { id: number }) => ImBotService.delete(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: imBotsKeys.list() }),
  });
}

/** 重启连接（被踢/挂死后恢复；状态投影在下次刷新可见）。 */
export function useRestartImBot() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: { id: number }) => ImBotService.restart(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: imBotsKeys.list() }),
  });
}

/** 会话级绑定/解绑（issueId 空 = 解绑；绑定每回合现读无需热重载）。成功后失效该 bot 的会话缓存。 */
export function useBindImBotIssue() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ImBotBindIssueRequest) => ImBotService.bindIssue(req),
    onSuccess: (_, req) => {
      void qc.invalidateQueries({ queryKey: imBotsKeys.conversations(req.botId) });
    },
  });
}

// ─── 扫码授权接入（provision）───
// 轮询节奏由组件按宿主下发的 pollIntervalMs 链式 setTimeout 驱动（协议节奏，非 TanStack
// refetchInterval）；connected 即激活完成（服务端已建 bot 并连接），此处只负责失效列表缓存。

/** 开新扫码会话（已有会话自动取消）。 */
export function useProvisionBegin() {
  return useMutation({
    mutationFn: () => ImBotService.provisionBegin(),
  });
}

/** 轮询一次扫码结果；connected 时失效列表（新 bot 已建）。 */
export function useProvisionPoll() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ImBotProvisionPollRequest) => ImBotService.provisionPoll(req),
    onSuccess: (view) => {
      if (view.state === 'connected') {
        void qc.invalidateQueries({ queryKey: imBotsKeys.list() });
      }
    },
  });
}

/** 取消扫码会话。 */
export function useProvisionCancel() {
  return useMutation({
    mutationFn: (req: ImBotProvisionCancelRequest) => ImBotService.provisionCancel(req),
  });
}
