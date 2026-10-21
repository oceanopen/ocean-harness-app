import type { StateCode } from '@src/state/tracker/stateMeta';
import type { WorkspaceLaunchSettings } from './WorkspaceService';
import type { WorkspaceTypeModel } from './WorkspaceTypeService';
import { request } from './http';

/**
 * issue 关联的单个「仓库+分支」项（多选）。localRepositoryId 须属于 issue 所属工作空间的
 * 关联仓库；repositoryBranch 为分支名（freeSolo 可手输）。
 */
export interface IssueRepositoryBranchModel {
  localRepositoryId: number;
  repositoryBranch: string;
}

export interface WorkspaceIssueResponseData {
  id: string; // uuid 字符串
  workspaceId: string; // 归属工作空间 uuid 字符串
  name: string;
  description: string; // issue 描述：markdown 源文本（前端 Milkdown 编辑器产出，非 HTML）
  stateCode: StateCode; // 固定 5 值枚举（BACKLOG/TODO/IN_PROGRESS/DONE/CANCELLED，见 stateMeta）
  sortOrder: number;
  startDate: string;
  targetDate: string;
  completedAt: string | null;
  typeId: number; // issue 类型 id（t_workspace_types 单值引用，0=未分类）
  createdAt: string;
  updatedAt: string;
  type: WorkspaceTypeModel | null; // 类型详情（typeId=0 或类型已删时为 null）
  repositoryBranchList: IssueRepositoryBranchModel[]; // 关联的仓库+分支列表（空数组=未关联）
  launchSettings?: WorkspaceLaunchSettings; // 启动设置覆盖（undefined = 未覆盖，字段级回落 workspace）
}

// POST /api/tracker/workspaceIssue/getList 的入参。
export interface WorkspaceIssueGetListRequest {
  workspaceId: string; // uuid 字符串
  orderBy?: string; // created_at/sort_order，空则 sort_order
  stateCode?: StateCode;
  typeId?: number; // >0 按类型筛选
  keyword?: string;
}

// POST /api/tracker/workspaceIssue/create 的入参（sortOrder 由后端自算，不入参）。
export interface WorkspaceIssueCreateRequest {
  workspaceId: string; // uuid 字符串
  name: string;
  description?: string;
  startDate?: string;
  targetDate?: string;
  stateCode?: StateCode; // 空值 → 后端默认 BACKLOG
  typeId?: number; // issue 类型（0/不传=未分类）
  repositoryBranchList?: IssueRepositoryBranchModel[]; // 全量覆盖关联的仓库+分支列表（逐项校验仓库归属）
  launchSettings?: WorkspaceLaunchSettings; // 启动设置覆盖（不传 = 未覆盖）
}

// POST /api/tracker/workspaceIssue/update 的入参（typeId 不传=保留原值，传值（含 0）=覆写）。
export interface WorkspaceIssueUpdateRequest {
  id: string;
  name: string;
  description?: string;
  stateCode?: StateCode;
  startDate?: string;
  targetDate?: string;
  typeId?: number;
  repositoryBranchList?: IssueRepositoryBranchModel[]; // 全量覆盖关联的仓库+分支列表（逐项校验仓库归属）
  launchSettings?: WorkspaceLaunchSettings; // 启动设置覆盖（不传 = 清除覆盖，字段级回落 workspace）
}

// POST /api/tracker/workspaceIssue/delete 的入参。
export interface WorkspaceIssueDeleteRequest {
  id: string;
}

// POST /api/tracker/workspaceIssue/move 的入参（看板拖拽单卡移动）。
export interface WorkspaceIssueMoveRequest {
  id: string;
  stateCode: StateCode;
  sortOrder: number;
}

export class WorkspaceIssueService {
  // getList：返回指定工作空间的 issue 列表（含类型）。
  static getList(req: WorkspaceIssueGetListRequest): Promise<WorkspaceIssueResponseData[]> {
    return request<WorkspaceIssueResponseData[]>('POST', '/api/tracker/workspaceIssue/getList', req);
  }

  // create：创建 issue，返回新建实体。
  static create(req: WorkspaceIssueCreateRequest): Promise<WorkspaceIssueResponseData> {
    return request<WorkspaceIssueResponseData>('POST', '/api/tracker/workspaceIssue/create', req);
  }

  // update：更新 issue，返回更新后的实体。
  static update(req: WorkspaceIssueUpdateRequest): Promise<WorkspaceIssueResponseData> {
    return request<WorkspaceIssueResponseData>('POST', '/api/tracker/workspaceIssue/update', req);
  }

  // delete：删除 issue。
  static delete(req: WorkspaceIssueDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/tracker/workspaceIssue/delete', req);
  }

  // move：看板拖拽单卡移动（改 stateCode + sortOrder），返回移动后的实体。
  static move(req: WorkspaceIssueMoveRequest): Promise<WorkspaceIssueResponseData> {
    return request<WorkspaceIssueResponseData>('POST', '/api/tracker/workspaceIssue/move', req);
  }
}
