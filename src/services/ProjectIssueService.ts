import type { WorkspaceLabelModel } from './WorkspaceLabelService';
import { request } from './http';

export type Priority = 'urgent' | 'high' | 'medium' | 'low' | 'none';

export interface ProjectIssueResponseData {
  id: number;
  projectId: number;
  workspaceId: number;
  name: string;
  description: string; // issue 描述：markdown 源文本（前端 Milkdown 编辑器产出，非 HTML）
  stateId: number;
  priority: Priority;
  sortOrder: number;
  parentId: number;
  startDate: string;
  targetDate: string;
  completedAt: string | null;
  isDraft: 'Y' | 'N';
  createdAt: string;
  updatedAt: string;
  labels: WorkspaceLabelModel[];
  localRepositoryId: number; // 关联的本地仓库 id（0=未关联）
  repositoryBranch: string; // 关联分支名（空串=未关联）
}

// POST /api/tracker/projectIssue/getList 的入参。
export interface ProjectIssueGetListRequest {
  projectId: number;
  orderBy?: string; // id/sort_order/priority/created_at，空则 sort_order
  stateId?: number;
  priority?: Priority;
  labelId?: number;
  keyword?: string;
}

// POST /api/tracker/projectIssue/create 的入参。
export interface ProjectIssueCreateRequest {
  projectId: number;
  workspaceId: number;
  name: string;
  description?: string;
  priority?: Priority;
  isDraft?: 'Y' | 'N';
  startDate?: string;
  targetDate?: string;
  stateId?: number; // 0 → 取 project.default_state_id
  parentId?: number; // 0=顶级，>0=子任务（须与父同 project，仅一层）
  labelIds?: number[];
  localRepositoryId?: number; // 0=未关联；>0 须属于当前项目关联仓库
  repositoryBranch?: string; // 分支名；localRepositoryId=0 时后端强制清空
}

// POST /api/tracker/projectIssue/update 的入参。
export interface ProjectIssueUpdateRequest {
  id: number;
  name: string;
  description?: string;
  stateId?: number;
  priority?: Priority;
  isDraft?: 'Y' | 'N';
  startDate?: string;
  targetDate?: string;
  labelIds?: number[];
  localRepositoryId?: number; // 0=清除关联；>0 须属于当前项目关联仓库
  repositoryBranch?: string; // 分支名；localRepositoryId=0 时后端强制清空
}

// POST /api/tracker/projectIssue/delete 的入参。
export interface ProjectIssueDeleteRequest {
  id: number;
}

// POST /api/tracker/projectIssue/move 的入参（看板拖拽单卡移动）。
export interface ProjectIssueMoveRequest {
  id: number;
  stateId: number;
  sortOrder: number;
}

// POST /api/tracker/projectIssue/updateState 的入参（编排推进状态，仅改 stateId 不改 sortOrder）。
export interface ProjectIssueUpdateStateRequest {
  id: number;
  stateId: number;
}

export class ProjectIssueService {
  // getList：返回指定项目的 issue 列表（含 labels）。
  static getList(req: ProjectIssueGetListRequest): Promise<ProjectIssueResponseData[]> {
    return request<ProjectIssueResponseData[]>('POST', '/api/tracker/projectIssue/getList', req);
  }

  // create：创建 issue，返回新建实体。
  static create(req: ProjectIssueCreateRequest): Promise<ProjectIssueResponseData> {
    return request<ProjectIssueResponseData>('POST', '/api/tracker/projectIssue/create', req);
  }

  // update：更新 issue，返回更新后的实体。
  static update(req: ProjectIssueUpdateRequest): Promise<ProjectIssueResponseData> {
    return request<ProjectIssueResponseData>('POST', '/api/tracker/projectIssue/update', req);
  }

  // delete：删除 issue。
  static delete(req: ProjectIssueDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/tracker/projectIssue/delete', req);
  }

  // move：看板拖拽单卡移动（改 stateId + sortOrder），返回移动后的实体。
  static move(req: ProjectIssueMoveRequest): Promise<ProjectIssueResponseData> {
    return request<ProjectIssueResponseData>('POST', '/api/tracker/projectIssue/move', req);
  }

  // updateState：编排推进状态（仅改 stateId，sortOrder 由后端保留原值），返回更新后的实体。
  static updateState(req: ProjectIssueUpdateStateRequest): Promise<ProjectIssueResponseData> {
    return request<ProjectIssueResponseData>('POST', '/api/tracker/projectIssue/updateState', req);
  }
}
