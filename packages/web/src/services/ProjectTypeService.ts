import { request } from './http';

/**
 * 项目类型（t_project_types，挂 project_id）：项目级预定义，
 * 任务经 t_project_tasks.type_id 单值引用（每任务至多一个类型）。
 */
export interface ProjectTypeModel {
  id: number;
  projectId: string; // 归属项目 uuid
  name: string;
  color: string;
  description: string;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
}

// POST /api/project/type/getList 的入参（按 projectId 查全部；路由随域更名归 T9.1）。
export interface ProjectTypeGetListRequest {
  projectId: string;
}

// POST /api/project/type/create 的入参。
export interface ProjectTypeCreateRequest {
  projectId: string;
  name: string;
  color?: string;
  description?: string;
}

// POST /api/project/type/update 的入参。
export interface ProjectTypeUpdateRequest {
  id: number;
  name: string;
  color?: string;
  description?: string;
}

// POST /api/project/type/delete 的入参。
export interface ProjectTypeDeleteRequest {
  id: number;
}

export class ProjectTypeService {
  // getList：返回指定项目下的全部类型（sort_order 升序）。
  static getList(req: ProjectTypeGetListRequest): Promise<ProjectTypeModel[]> {
    return request<ProjectTypeModel[]>('POST', '/api/project/type/getList', req);
  }

  // create：创建类型（后端 sort_order 自算 MAX+10000），返回新建实体。
  static create(req: ProjectTypeCreateRequest): Promise<ProjectTypeModel> {
    return request<ProjectTypeModel>('POST', '/api/project/type/create', req);
  }

  // update：更新类型（仅 name/color/description），返回更新后的实体。
  static update(req: ProjectTypeUpdateRequest): Promise<ProjectTypeModel> {
    return request<ProjectTypeModel>('POST', '/api/project/type/update', req);
  }

  // delete：删除类型（后端将引用该类型的任务置为未分类）。
  static delete(req: ProjectTypeDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/project/type/delete', req);
  }
}
