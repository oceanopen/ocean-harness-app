import type {
  ConversationCreateRequest,
  ConversationDeleteRequest,
  ConversationUpdateRequest,
} from '@src/services';
import { ConversationService } from '@src/services';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { conversationKeys } from './keys';

// ─── 读取（query）───

/** 指定归属实体下的全部会话。ownerId 为 null/空串时不查询（无效请求守卫）。 */
export function useConversations(ownerType: 'workspace' | 'task', ownerId: string | null) {
  return useQuery({
    queryKey: conversationKeys.conversations(ownerType, ownerId ?? ''),
    queryFn: () => ConversationService.getList({ ownerType, ownerId: ownerId! }),
    enabled: ownerId != null && ownerId !== '',
  });
}

// ─── 写操作（mutation）───
/**
 * 每个 mutation 内部 invalidate 对应 key；消费方只调 mutateAsync/mutate，不关心失效。
 * 失效定位：create/update 从返回实体取归属；delete 实体已不在，按整域根失效兜底（会话量小，代价可忽略）。
 */

/** 创建会话。 */
export function useCreateConversation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ConversationCreateRequest) => ConversationService.create(req),
    onSuccess: created =>
      qc.invalidateQueries({
        queryKey: conversationKeys.conversations(created.ownerType, created.ownerId),
      }),
  });
}

/** 更新会话标题。 */
export function useUpdateConversation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ConversationUpdateRequest) => ConversationService.update(req),
    onSuccess: updated =>
      qc.invalidateQueries({
        queryKey: conversationKeys.conversations(updated.ownerType, updated.ownerId),
      }),
  });
}

/** 删除会话行。 */
export function useDeleteConversation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ConversationDeleteRequest) => ConversationService.delete(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: conversationKeys.root }),
  });
}
