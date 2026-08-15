import { request } from './http';

export interface WorkspaceModel {
  id: number;
  name: string;
  slug: string;
  description: string;
  createdAt: string;
  updatedAt: string;
  deletedAt: string | null;
}

// POST /api/tracker/workspace/getList 的入参（当前无参，预留筛选位）。
export interface WorkspaceGetListRequest {}

// POST /api/tracker/workspace/create 的入参。
export interface WorkspaceCreateRequest {
  name: string;
  slug: string;
  description?: string;
}

// POST /api/tracker/workspace/update 的入参。
export interface WorkspaceUpdateRequest {
  id: number;
  name: string;
  slug: string;
  description?: string;
}

// POST /api/tracker/workspace/delete 的入参。
export interface WorkspaceDeleteRequest {
  id: number;
}

export class WorkspaceService {
  // getList：返回全部工作空间。
  static getList(req: WorkspaceGetListRequest = {}): Promise<WorkspaceModel[]> {
    return request<WorkspaceModel[]>('POST', '/api/tracker/workspace/getList', req);
  }

  // create：创建工作空间（恢复式 upsert），返回新建实体。
  static create(req: WorkspaceCreateRequest): Promise<WorkspaceModel> {
    return request<WorkspaceModel>('POST', '/api/tracker/workspace/create', req);
  }

  // update：更新工作空间，返回更新后的实体。
  static update(req: WorkspaceUpdateRequest): Promise<WorkspaceModel> {
    return request<WorkspaceModel>('POST', '/api/tracker/workspace/update', req);
  }

  // delete：删除工作空间。
  static delete(req: WorkspaceDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/tracker/workspace/delete', req);
  }
}
