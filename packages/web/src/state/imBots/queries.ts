import type { ImBotCreateRequest, ImBotModel, ImBotUpdateRequest } from '@src/services';
import { ImBotService } from '@src/services';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { imBotsKeys } from './keys';

// ─── 读取（query）───
// 手动刷新模型（对齐项目既定决策）：操作后由 mutation invalidate 自动刷新，页面提供手动
// 刷新入口；不挂轮询——连接状态变化在两次操作之间仅靠手动刷新可见（Tauri 事件推送为后续增强，
// imBotsKeys.root 已预留整域失效根）。

/** 全部 IM bot（含运行态投影）。 */
export function useImBots() {
  return useQuery({
    queryKey: imBotsKeys.list(),
    queryFn: () => ImBotService.getList(),
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
