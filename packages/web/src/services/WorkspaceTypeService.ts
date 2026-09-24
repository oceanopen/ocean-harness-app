import { request } from './http';

// 原「标签」（WorkspaceLabel）域，2026-09-24 更名为「类型」：workspace 级预定义，
// issue 经 t_project_issues.type_id 单值引用（每 issue 至多一个类型）。
export interface WorkspaceTypeModel {
  id: number;
  workspaceId: number;
  name: string;
  color: string;
  description: string;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
}

// POST /api/tracker/workspaceType/getList 的入参（按 workspaceId 查全部）。
export interface WorkspaceTypeGetListRequest {
  workspaceId: number;
}

// POST /api/tracker/workspaceType/create 的入参。
export interface WorkspaceTypeCreateRequest {
  workspaceId: number;
  name: string;
  color?: string;
  description?: string;
}

// POST /api/tracker/workspaceType/update 的入参。
export interface WorkspaceTypeUpdateRequest {
  id: number;
  name: string;
  color?: string;
  description?: string;
}

// POST /api/tracker/workspaceType/delete 的入参。
export interface WorkspaceTypeDeleteRequest {
  id: number;
}

export class WorkspaceTypeService {
  // getList：返回指定工作空间下的全部类型。
  static getList(req: WorkspaceTypeGetListRequest): Promise<WorkspaceTypeModel[]> {
    return request<WorkspaceTypeModel[]>('POST', '/api/tracker/workspaceType/getList', req);
  }

  // create：创建类型，返回新建实体。
  static create(req: WorkspaceTypeCreateRequest): Promise<WorkspaceTypeModel> {
    return request<WorkspaceTypeModel>('POST', '/api/tracker/workspaceType/create', req);
  }

  // update：更新类型，返回更新后的实体。
  static update(req: WorkspaceTypeUpdateRequest): Promise<WorkspaceTypeModel> {
    return request<WorkspaceTypeModel>('POST', '/api/tracker/workspaceType/update', req);
  }

  // delete：删除类型（后端将引用该类型的 issue 置为未分类）。
  static delete(req: WorkspaceTypeDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/tracker/workspaceType/delete', req);
  }
}
