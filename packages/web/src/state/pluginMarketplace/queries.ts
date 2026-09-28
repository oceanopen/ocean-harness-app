import type {
  PluginMarketplaceAddRequest,
  PluginMarketplaceListModel,
  PluginMarketplaceRemoveRequest,
  PluginMarketplaceUpdateRequest,
  PluginOperationRequest,
} from '@src/services';
import { PluginMarketplaceService } from '@src/services';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { pluginMarketplaceKeys } from './keys';

// ─── 读取（query）───

/** 全部插件市场投影（市场注册表 × 清单扫描 × 安装状态，SSOT 恒为 claude 侧）。 */
export function usePluginMarketplaces() {
  return useQuery({
    queryKey: pluginMarketplaceKeys.list(),
    queryFn: () => PluginMarketplaceService.getList(),
  });
}

// ─── 写操作（mutation）───
// 后端所有写操作统一返回最新列表投影，故全部走 setQueryData 整体替换缓存
// （一次往返即完成刷新，无需 invalidate 触发二次请求）。

/** 写操作公共装配：mutationFn 返回最新列表，成功后就地替换 list 缓存。 */
function useListReplacingMutation<TReq>(fn: (req: TReq) => Promise<PluginMarketplaceListModel>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: list => qc.setQueryData<PluginMarketplaceListModel>(pluginMarketplaceKeys.list(), list),
  });
}

/** 注册插件市场（source：本地绝对路径 / GitHub owner/repo / git URL）。 */
export function useAddPluginMarketplace() {
  return useListReplacingMutation<PluginMarketplaceAddRequest>(req => PluginMarketplaceService.add(req));
}

/** 刷新插件市场（github/git 源重新拉取；本地源重校验清单）。 */
export function useUpdatePluginMarketplace() {
  return useListReplacingMutation<PluginMarketplaceUpdateRequest>(req => PluginMarketplaceService.update(req));
}

/** 注销插件市场（连带卸载该市场全部已装插件，调用方须先经用户确认）。 */
export function useRemovePluginMarketplace() {
  return useListReplacingMutation<PluginMarketplaceRemoveRequest>(req => PluginMarketplaceService.remove(req));
}

/** 安装插件（user scope 全局）。 */
export function useInstallPlugin() {
  return useListReplacingMutation<PluginOperationRequest>(req => PluginMarketplaceService.install(req));
}

/** 卸载插件。 */
export function useUninstallPlugin() {
  return useListReplacingMutation<PluginOperationRequest>(req => PluginMarketplaceService.uninstall(req));
}

/** 启用插件。 */
export function useEnablePlugin() {
  return useListReplacingMutation<PluginOperationRequest>(req => PluginMarketplaceService.enable(req));
}

/** 禁用插件（保留安装）。 */
export function useDisablePlugin() {
  return useListReplacingMutation<PluginOperationRequest>(req => PluginMarketplaceService.disable(req));
}

/** 更新插件到市场最新版本。 */
export function useUpdatePluginOp() {
  return useListReplacingMutation<PluginOperationRequest>(req => PluginMarketplaceService.updatePlugin(req));
}
