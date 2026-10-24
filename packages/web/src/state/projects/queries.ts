import type {
  ProjectCreateRequest,
  ProjectDeleteRequest,
  ProjectUpdateRequest,
} from '@src/services';
import { ProjectService } from '@src/services';
import { imBotsKeys } from '@src/state/imBots/keys';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { projectsKeys } from './keys';

// ─── 读取（query）───

/** 全部项目（id 倒序，含关联仓库 id）。 */
export function useProjects() {
  return useQuery({
    queryKey: projectsKeys.projects(),
    queryFn: () => ProjectService.getList(),
  });
}

// ─── 写操作（mutation）───
/**
 * 每个 mutation 内部 invalidate 对应 key；消费方只调 mutateAsync/mutate，不关心失效。
 * delete 额外失效 imBots 域（bot 的 project_id/task_id 被后端清列，bot 卡片关联展示需刷新）。
 */

/** 创建项目。 */
export function useCreateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ProjectCreateRequest) => ProjectService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: projectsKeys.projects() }),
  });
}

/** 更新项目（目录变更后端热更新关联 bot 连接）。 */
export function useUpdateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ProjectUpdateRequest) => ProjectService.update(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: projectsKeys.projects() }),
  });
}

/** 删除项目（级联任务/会话行/类型/仓库关联，bot 清列）。 */
export function useDeleteProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ProjectDeleteRequest) => ProjectService.delete(req),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: projectsKeys.root });
      void qc.invalidateQueries({ queryKey: imBotsKeys.list() });
    },
  });
}
