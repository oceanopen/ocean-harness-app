import { request } from './http';

// WorkspaceProjectModel：对齐后端 model.WorkspaceProject 的 JSON 形态（t_workspace_projects）。
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
}

// POST /api/tracker/project/getList 的入参（按 workspaceId 查）。
export interface WorkspaceProjectGetListRequest {
  workspaceId: number;
}

// POST /api/tracker/project/create 的入参。
export interface WorkspaceProjectCreateRequest {
  workspaceId: number;
  name: string;
  description?: string;
  emoji?: string;
}

// POST /api/tracker/project/update 的入参。
export interface WorkspaceProjectUpdateRequest {
  id: number;
  name: string;
  description?: string;
  emoji?: string;
}

// POST /api/tracker/project/delete 的入参。
export interface WorkspaceProjectDeleteRequest {
  id: number;
}

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
