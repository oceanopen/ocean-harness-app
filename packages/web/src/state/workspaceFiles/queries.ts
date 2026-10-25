/**
 * workspaceFiles 域 server 状态（TanStack Query）：文件树 + 文件内容。
 * 手动刷新口径（与 T3.1 子任务面板同决策）：无 watcher 无轮询，树刷新靠面板刷新按钮显式 invalidate；内容靠 staleTime 0 的激活重验。
 */

import { TaskWorkspaceService } from '@src/services';
import { useQuery } from '@tanstack/react-query';
import { workspaceFilesKeys } from './keys';

/*
 * 查看归属入参（taskId / workspaceId 互斥，五端点同款）：workspaceId 在场走工作空间
 * 目录本身，否则走任务两跳；缓存 key 仍以锚定 id（issueId ?? workspaceId）分段。
 */
function fileScope(issueId: string | null, workspaceId?: string): { taskId: string } | { workspaceId: string } {
  return workspaceId != null && workspaceId !== ''
    ? { workspaceId }
    : { taskId: issueId ?? '' };
}

/**
 * 工作空间文件树（一次性全树扁平节点表，前端纯函数组树）。
 */
export function useWorkspaceFileTree(issueId: string | null, workspaceId?: string) {
  return useQuery({
    queryKey: workspaceFilesKeys.tree(issueId ?? workspaceId ?? ''),
    queryFn: () => TaskWorkspaceService.fileTree(fileScope(issueId, workspaceId)),
    enabled: issueId != null || (workspaceId != null && workspaceId !== ''),
  });
}

/**
 * 文件内容（预览浮层激活 tab 消费）。staleTime 显式 0（覆盖全局 10min 默认）：agent 在
 * 终端持续改文件，tab 每次挂载/激活都静默重验——SWR 语义，缓存命中期间先显示旧值无
 * spinner，重验到位后原位替换。gcTime 走默认 5min，关 tab 后自然回收。
 */
export function useWorkspaceFileContent(issueId: string | null, path: string | null, workspaceId?: string) {
  return useQuery({
    queryKey: workspaceFilesKeys.content(issueId ?? workspaceId ?? '', path ?? ''),
    queryFn: async () => {
      const t0 = performance.now();
      const data = await TaskWorkspaceService.fileContent({ ...fileScope(issueId, workspaceId), path: path! });
      if (import.meta.env.DEV) {
        console.info(`[workspaceFiles] content fetch ${path} ${Math.round(performance.now() - t0)}ms`);
      }
      return data;
    },
    enabled: (issueId != null || (workspaceId != null && workspaceId !== '')) && path != null && path !== '',
    staleTime: 0,
  });
}

/**
 * Git 变更列表（文件面板「Git 变更」模式消费；workspaceId 归属时兼任 isGitRepo 探测）。
 * staleTime 0 与 content 同口径——agent 持续改文件，模式切换/面板重挂载时静默重验。
 */
export function useWorkspaceGitChanges(issueId: string | null, workspaceId?: string) {
  return useQuery({
    queryKey: workspaceFilesKeys.gitChanges(issueId ?? workspaceId ?? ''),
    queryFn: () => TaskWorkspaceService.gitChanges(fileScope(issueId, workspaceId)),
    enabled: issueId != null || (workspaceId != null && workspaceId !== ''),
    staleTime: 0,
  });
}

/**
 * 单文件未提交变更内容对（预览浮层 diff tab 消费）。staleTime 0 与 content 同口径。
 */
export function useWorkspaceFileDiff(issueId: string | null, path: string | null, workspaceId?: string) {
  return useQuery({
    queryKey: workspaceFilesKeys.fileDiff(issueId ?? workspaceId ?? '', path ?? ''),
    queryFn: () => TaskWorkspaceService.fileDiff({ ...fileScope(issueId, workspaceId), path: path! }),
    enabled: (issueId != null || (workspaceId != null && workspaceId !== '')) && path != null && path !== '',
    staleTime: 0,
  });
}
