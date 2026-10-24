import type { ConversationModel, ProjectTaskResponseData } from '@src/services';
import type { WorkbenchContainer } from './WorkbenchContent';
import { mergeLaunchSettings } from '@src/shared/launchSettings';
import { useConversations, useCreateConversation } from '@src/state/conversation';
import { useEffect, useMemo, useRef, useState } from 'react';

export interface TaskMainConversationState {
  // 主会话行（parentId 空且 created_at 最早）；null = 未就绪（闸门等待）。
  main: ConversationModel | null;
  // 解析失败（列表查询或创建报错，二取一）；重试经 retry。
  error: Error | null;
  // 解锁重试：清除失败登记并重查列表（失败后不自动重试，防错误风暴）。
  retry: () => void;
}

/**
 * useTaskMainConversation（T6.2）：任务主会话单飞编排——读列表取最早主会话，无则在途
 * 单发创建（mode 按容器/任务 mergeLaunchSettings 现算）。container/task 任一为 null 即禁用。
 * 单飞两层：实例内在途 promise 登记（effect 重跑/并发渲染去重）+ 列表定态门槛
 * （isFetching 期间不建，吸收他窗口 create 失效后的回读）；跨窗口并发建由
 * 「锚非空不覆盖/首捕获胜」的既有服务端语义兜底。
 */
export function useTaskMainConversation(
  container: WorkbenchContainer | null,
  task: ProjectTaskResponseData | null,
): TaskMainConversationState {
  const taskId = task?.id ?? null;
  const list = useConversations('task', taskId);
  const create = useCreateConversation();
  const [retrySeed, setRetrySeed] = useState(0);
  // ownerId → 在途创建 promise（渲染期读取仅为派生闸门，真值由 effect 维护）。
  const creatingRef = useRef(new Map<string, Promise<unknown>>());
  const failedRef = useRef(new Set<string>());

  const main = useMemo(() => {
    const mains = (list.data ?? []).filter(c => c.parentId === '');
    if (mains.length === 0) {
      return null;
    }
    // 兜底重排（契约 created_at 升序；不信任远端排序）。
    return [...mains].sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1))[0];
  }, [list.data]);

  useEffect(() => {
    if (container == null || taskId == null || list.isPending || list.isFetching) {
      return;
    }
    if (main != null || creatingRef.current.has(taskId) || failedRef.current.has(taskId)) {
      return;
    }
    const mode = mergeLaunchSettings(container.launchSettings, task?.launchSettings)?.mode === 'acp' ? 'acp' : 'terminal';
    // 拒绝就地吞掉（错误由 create.error 承载供呈现），存档 promise 仅作在途标记无人 await。
    const promise = create
      .mutateAsync({ ownerType: 'task', ownerId: taskId, mode })
      .then(() => undefined, (_e: unknown) => {
        failedRef.current.add(taskId);
      })
      .finally(() => {
        if (creatingRef.current.get(taskId) === promise) {
          creatingRef.current.delete(taskId);
        }
      });
    creatingRef.current.set(taskId, promise);
  }, [container, taskId, task?.launchSettings, main, list.isPending, list.isFetching, list.data, create, retrySeed]);

  const retry = () => {
    if (taskId != null) {
      failedRef.current.delete(taskId);
    }
    void list.refetch();
    setRetrySeed(s => s + 1);
  };

  return { main, error: list.error ?? create.error ?? null, retry };
}
