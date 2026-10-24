import type { ProjectTypeModel } from './ProjectTypeService';
import type { WorkspaceLaunchSettings } from './WorkspaceService';
import { request } from './http';

/**
 * issue 关联的单个「仓库+分支」项（多选）。localRepositoryId 须属于 issue 所属工作空间的
 * 关联仓库；repositoryBranch 为分支名（freeSolo 可手输）。
 */
export interface IssueRepositoryBranchModel {
  localRepositoryId: number;
  repositoryBranch: string;
}

export interface ProjectTaskResponseData {
  id: string; // uuid 字符串
  projectId: string; // 归属工作空间 uuid 字符串
  name: string;
  description: string; // issue 描述：markdown 源文本（前端 Milkdown 编辑器产出，非 HTML）
  stateCode: string; // 固定 5 值枚举（BACKLOG/TODO/IN_PROGRESS/DONE/CANCELLED）
  sortOrder: number;
  startDate: string;
  targetDate: string;
  completedAt: string | null;
  typeId: number; // issue 类型 id（t_workspace_types 单值引用，0=未分类）
  createdAt: string;
  updatedAt: string;
  type: ProjectTypeModel | null; // 类型详情（typeId=0 或类型已删时为 null）
  repositoryBranchList: IssueRepositoryBranchModel[]; // 关联的仓库+分支列表（空数组=未关联）
  launchSettings?: WorkspaceLaunchSettings; // 启动设置覆盖（undefined = 未覆盖，字段级回落 workspace）
}

// POST /api/project/task/getList 的入参。
export interface ProjectTaskGetListRequest {
  projectId: string; // uuid 字符串
  orderBy?: string; // created_at/sort_order，空则 sort_order
  stateCode?: string;
  typeId?: number; // >0 按类型筛选
  keyword?: string;
}

// POST /api/project/task/create 的入参（sortOrder 由后端自算，不入参）。
export interface ProjectTaskCreateRequest {
  projectId: string; // uuid 字符串
  name: string;
  description?: string;
  startDate?: string;
  targetDate?: string;
  stateCode?: string; // 空值 → 后端默认 BACKLOG
  typeId?: number; // issue 类型（0/不传=未分类）
  repositoryBranchList?: IssueRepositoryBranchModel[]; // 全量覆盖关联的仓库+分支列表（逐项校验仓库归属）
  launchSettings?: WorkspaceLaunchSettings; // 启动设置覆盖（不传 = 未覆盖）
}

// POST /api/project/task/update 的入参（typeId 不传=保留原值，传值（含 0）=覆写）。
export interface ProjectTaskUpdateRequest {
  id: string;
  name: string;
  description?: string;
  stateCode?: string;
  startDate?: string;
  targetDate?: string;
  typeId?: number;
  repositoryBranchList?: IssueRepositoryBranchModel[]; // 全量覆盖关联的仓库+分支列表（逐项校验仓库归属）
  launchSettings?: WorkspaceLaunchSettings; // 启动设置覆盖（不传 = 清除覆盖，字段级回落 workspace）
}

// POST /api/project/task/delete 的入参。
export interface ProjectTaskDeleteRequest {
  id: string;
}

// POST /api/project/task/move 的入参（看板拖拽单卡移动）。
export interface WorkspaceIssueMoveRequest {
  id: string;
  stateCode: string;
  sortOrder: number;
}

export class ProjectTaskService {
  // getList：返回指定工作空间的 issue 列表（含类型）。
  static getList(req: ProjectTaskGetListRequest): Promise<ProjectTaskResponseData[]> {
    return request<ProjectTaskResponseData[]>('POST', '/api/project/task/getList', req);
  }

  // create：创建 issue，返回新建实体。
  static create(req: ProjectTaskCreateRequest): Promise<ProjectTaskResponseData> {
    return request<ProjectTaskResponseData>('POST', '/api/project/task/create', req);
  }

  // update：更新 issue，返回更新后的实体。
  static update(req: ProjectTaskUpdateRequest): Promise<ProjectTaskResponseData> {
    return request<ProjectTaskResponseData>('POST', '/api/project/task/update', req);
  }

  // delete：删除 issue。
  static delete(req: ProjectTaskDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/project/task/delete', req);
  }

  // move：看板拖拽单卡移动（改 stateCode + sortOrder），返回移动后的实体。
  static move(req: WorkspaceIssueMoveRequest): Promise<ProjectTaskResponseData> {
    return request<ProjectTaskResponseData>('POST', '/api/project/task/move', req);
  }
}
