import type {
  LocalRepositoryCreateRequest,
  LocalRepositoryModel,
  LocalRepositoryUpdateRequest,
} from '@src/services';
import { LocalRepositoryService } from '@src/services';
import { trackerKeys } from '@src/state/tracker';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { localRepositoryKeys } from './keys';

// ─── 读取（query）───

/** 全部本地仓库（按 lastCommitAt 倒序、id 升序）。供仓库页与迭代 2 项目-仓库选择器复用。 */
export function useLocalRepositories() {
  return useQuery({
    queryKey: localRepositoryKeys.list(),
    queryFn: () => LocalRepositoryService.getList(),
  });
}

/** 指定仓库的本地分支列表（git branch 默认仅本地分支），供 issue 分支选择器。localRepositoryId<=0 时不查询。 */
export function useLocalBranches(localRepositoryId: number) {
  return useQuery({
    queryKey: localRepositoryKeys.localBranches(localRepositoryId),
    queryFn: () => LocalRepositoryService.getLocalBranches({ id: localRepositoryId }),
    enabled: localRepositoryId > 0,
  });
}

// ─── 写操作（mutation）───
// create/update/delete 走 invalidate（与 tracker 域一致）；refresh/refreshAll 返回最新数据，
// 直接 setQueryData 更新缓存（无需额外 refetch）。

/** 新建本地仓库。 */
export function useCreateLocalRepository() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: LocalRepositoryCreateRequest) => LocalRepositoryService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: localRepositoryKeys.list() }),
  });
}

/** 更新本地仓库。 */
export function useUpdateLocalRepository() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: LocalRepositoryUpdateRequest) => LocalRepositoryService.update(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: localRepositoryKeys.list() }),
  });
}

/**
 * 物理删除本地仓库。入参为 id（与 tracker 域 useDelete* 签名一致）。
 * 后端删仓库会事务级联：硬删项目↔仓库中间表记录 + 清空指向该仓库的 issue.localRepositoryId/repositoryBranch，
 * 故除本地仓库列表外还需失效 tracker 整域（projectRepositories/projectIssues），否则 UI 残留悬挂引用。
 */
export function useDeleteLocalRepository() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => LocalRepositoryService.delete({ id }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: localRepositoryKeys.list() });
      qc.invalidateQueries({ queryKey: trackerKeys.root });
    },
  });
}

/** 刷新单个仓库：用返回值就地替换缓存中对应项。variables.id 用于卡片级 loading 判定。 */
export function useRefreshLocalRepository() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: { id: number }) => LocalRepositoryService.refresh(req),
    onSuccess: (updated) => {
      qc.setQueryData<LocalRepositoryModel[]>(localRepositoryKeys.list(), old =>
        (old ?? []).map(r => (r.id === updated.id ? updated : r)));
    },
  });
}

/** 全量刷新：用返回的完整列表替换缓存；loading 由消费方用 isPending 驱动（同 useRefreshLocalRepository）。 */
export function useRefreshAllLocalRepositories() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => LocalRepositoryService.refreshAll(),
    onSuccess: list => qc.setQueryData<LocalRepositoryModel[]>(localRepositoryKeys.list(), list),
  });
}
