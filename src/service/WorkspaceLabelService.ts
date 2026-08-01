import { request } from './http';

export interface WorkspaceLabelModel {
  id: number;
  workspaceId: number;
  name: string;
  color: string;
  description: string;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
  deletedAt: string | null;
}

// POST /api/tracker/workspaceLabel/getList 的入参（按 workspaceId 查全部）。
export interface WorkspaceLabelGetListRequest {
  workspaceId: number;
}

// POST /api/tracker/workspaceLabel/create 的入参。
export interface WorkspaceLabelCreateRequest {
  workspaceId: number;
  name: string;
  color?: string;
  description?: string;
}

// POST /api/tracker/workspaceLabel/update 的入参。
export interface WorkspaceLabelUpdateRequest {
  id: number;
  name: string;
  color?: string;
  description?: string;
}

// POST /api/tracker/workspaceLabel/delete 的入参。
export interface WorkspaceLabelDeleteRequest {
  id: number;
}

export class WorkspaceLabelService {
  // getList：返回指定工作空间下的全部标签。
  static getList(req: WorkspaceLabelGetListRequest): Promise<WorkspaceLabelModel[]> {
    return request<WorkspaceLabelModel[]>('POST', '/api/tracker/workspaceLabel/getList', req);
  }

  // create：创建标签，返回新建实体。
  static create(req: WorkspaceLabelCreateRequest): Promise<WorkspaceLabelModel> {
    return request<WorkspaceLabelModel>('POST', '/api/tracker/workspaceLabel/create', req);
  }

  // update：更新标签，返回更新后的实体。
  static update(req: WorkspaceLabelUpdateRequest): Promise<WorkspaceLabelModel> {
    return request<WorkspaceLabelModel>('POST', '/api/tracker/workspaceLabel/update', req);
  }

  // delete：删除标签。
  static delete(req: WorkspaceLabelDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/tracker/workspaceLabel/delete', req);
  }
}
