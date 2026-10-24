import type { WorkspaceLaunchSettings } from './WorkspaceService';
import { request } from './http';

/**
 * ProjectModel：项目实体（独立实体线：项目 → 任务 → 主/子会话，任务运行目录根 = {dir}/{taskId}/）。
 * localRepositoryIds 为关联本地仓库全量列表（create/update 无 diff 全量覆盖，空 = 清空）。
 */
export interface ProjectModel {
  id: string; // uuid v7
  name: string;
  dir: string;
  description: string;
  emoji: string; // 展示图标（空串 = 前端图标兜底）
  launchSettings?: WorkspaceLaunchSettings;
  localRepositoryIds: number[];
  createdAt: string;
  updatedAt: string;
}

// POST /api/project/getList 的入参（当前无参，预留筛选位）。
export interface ProjectGetListRequest {}

// POST /api/project/getInfo 的入参。
export interface ProjectGetInfoRequest {
  id: string;
}

// POST /api/project/create 的入参（dir 须为绝对路径，后端校验可创建可写）。
export interface ProjectCreateRequest {
  name: string;
  dir: string;
  description?: string;
  emoji?: string;
  launchSettings?: WorkspaceLaunchSettings;
  localRepositoryIds?: number[];
}

// POST /api/project/update 的入参（launchSettings 省略 = 清空配置）。
export interface ProjectUpdateRequest {
  id: string;
  name: string;
  dir: string;
  description?: string;
  emoji?: string;
  launchSettings?: WorkspaceLaunchSettings;
  localRepositoryIds?: number[];
}

// POST /api/project/delete 的入参（级联任务/会话行/类型/仓库关联，bot 清列）。
export interface ProjectDeleteRequest {
  id: string;
}

export class ProjectService {
  // getList：全部项目（id 倒序，含关联仓库 id）。
  static getList(_req?: ProjectGetListRequest): Promise<ProjectModel[]> {
    return request<ProjectModel[]>('POST', '/api/project/getList', {});
  }

  // getInfo：按 id 取单个项目（含关联仓库 id）。
  static getInfo(req: ProjectGetInfoRequest): Promise<ProjectModel> {
    return request<ProjectModel>('POST', '/api/project/getInfo', req);
  }

  // create：新建项目。
  static create(req: ProjectCreateRequest): Promise<ProjectModel> {
    return request<ProjectModel>('POST', '/api/project/create', req);
  }

  // update：更新项目（目录变更后端热更新关联 bot 连接）。
  static update(req: ProjectUpdateRequest): Promise<ProjectModel> {
    return request<ProjectModel>('POST', '/api/project/update', req);
  }

  // delete：删除项目（级联任务/会话行/类型/仓库关联，bot 清列；claude 本地文件不动）。
  static delete(req: ProjectDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/project/delete', req);
  }
}
