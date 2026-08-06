import type { ProjectStateItem } from './ProjectStateService';
import { request } from './http';

// WorkspaceProjectModel：对齐后端 types.ProjectResponseData 的 JSON 形态（嵌入 DO 平铺 + localRepositoryIds 装配）。
export interface WorkspaceProjectModel {
  id: number;
  workspaceId: number;
  name: string;
  description: string;
  emoji: string;
  defaultStateId: number;
  createdAt: string;
  updatedAt: string;
  deletedAt: string | null;
  localRepositoryIds: number[]; // 关联的本地仓库 id 列表（随 getList/create/update 返回）
}

// POST /api/tracker/project/getList 的入参（按 workspaceId 查）。
export interface WorkspaceProjectGetListRequest {
  workspaceId: number;
}

// POST /api/tracker/project/create 的入参。
// localRepositoryIds 为关联仓库最终列表（全量，随项目一起保存，后端事务内全量写入）。
// states 为项目初始状态全量列表（引用目录，事务内全量写入 + 回填 default_state_id）。
export interface WorkspaceProjectCreateRequest {
  workspaceId: number;
  name: string;
  description?: string;
  emoji?: string;
  localRepositoryIds?: number[];
  states?: ProjectStateItem[];
}

// POST /api/tracker/project/update 的入参。
// localRepositoryIds 为关联仓库最终列表（全量覆盖：后端先删后插，无 diff）。
// states 非 undefined 时全量替换项目状态并重算 default_state_id；省略则跳过（不动状态）。
export interface WorkspaceProjectUpdateRequest {
  id: number;
  name: string;
  description?: string;
  emoji?: string;
  localRepositoryIds?: number[];
  states?: ProjectStateItem[];
}

// POST /api/tracker/project/delete 的入参。
export interface WorkspaceProjectDeleteRequest {
  id: number;
}

// 项目 ↔ 本地仓库 多对多关联：随 create/update 全量保存（无独立增删接口），ids 随项目响应返回。

export class WorkspaceProjectService {
  // getList：返回指定工作空间下的全部项目。
  static getList(req: WorkspaceProjectGetListRequest): Promise<WorkspaceProjectModel[]> {
    return request<WorkspaceProjectModel[]>('POST', '/api/tracker/project/getList', req);
  }

  // create：创建项目，返回新建实体。
  static create(req: WorkspaceProjectCreateRequest): Promise<WorkspaceProjectModel> {
    return request<WorkspaceProjectModel>('POST', '/api/tracker/project/create', req);
  }

  // update：更新项目，返回更新后的实体。
  static update(req: WorkspaceProjectUpdateRequest): Promise<WorkspaceProjectModel> {
    return request<WorkspaceProjectModel>('POST', '/api/tracker/project/update', req);
  }

  // delete：删除项目。
  static delete(req: WorkspaceProjectDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/tracker/project/delete', req);
  }
}
