import type { StateCode } from '@src/state/tracker/stateMeta';
import type { WorkspaceLabelModel } from './WorkspaceLabelService';
import { request } from './http';

export type Priority = 'urgent' | 'high' | 'medium' | 'low' | 'none';

export interface ProjectIssueResponseData {
  id: string; // uuid 字符串
  projectId: number;
  workspaceId: number;
  name: string;
  description: string; // issue 描述：markdown 源文本（前端 Milkdown 编辑器产出，非 HTML）
  stateCode: StateCode; // 固定 5 值枚举（BACKLOG/TODO/IN_PROGRESS/DONE/CANCELLED，见 stateMeta）
  priority: Priority;
  sortOrder: number;
  parentId: string; // 父任务 uuid（空串=顶级）
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
  orderBy?: string; // created_at/sort_order/priority，空则 sort_order
  stateCode?: StateCode;
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
  stateCode?: StateCode; // 空值 → 后端默认 BACKLOG
  parentId?: string; // 空串=顶级，非空=子任务（须与父同 project，仅一层）
  labelIds?: number[];
  localRepositoryId?: number; // 0=未关联；>0 须属于当前项目关联仓库
  repositoryBranch?: string; // 分支名；localRepositoryId=0 时后端强制清空
}

// POST /api/tracker/projectIssue/update 的入参。
export interface ProjectIssueUpdateRequest {
  id: string;
  name: string;
  description?: string;
  stateCode?: StateCode;
  priority?: Priority;
  isDraft?: 'Y' | 'N';
  startDate?: string;
  targetDate?: string;
  labelIds?: number[];
  localRepositoryId?: number; // 0=清除关联；>0 须属于当前项目关联仓库
  repositoryBranch?: string; // localRepositoryId=0 时后端强制清空
}

// POST /api/tracker/projectIssue/delete 的入参。
export interface ProjectIssueDeleteRequest {
  id: string;
}

// POST /api/tracker/projectIssue/move 的入参（看板拖拽单卡移动）。
export interface ProjectIssueMoveRequest {
  id: string;
  stateCode: StateCode;
  sortOrder: number;
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

  // move：看板拖拽单卡移动（改 stateCode + sortOrder），返回移动后的实体。
  static move(req: ProjectIssueMoveRequest): Promise<ProjectIssueResponseData> {
    return request<ProjectIssueResponseData>('POST', '/api/tracker/projectIssue/move', req);
  }
}
