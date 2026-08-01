import { request } from './http';

export type StateGroup = 'backlog' | 'unstarted' | 'started' | 'completed' | 'cancelled';

export interface ProjectStateModel {
  id: number;
  projectId: number;
  workspaceId: number;
  name: string;
  color: string;
  slug: string;
  stateGroup: StateGroup;
  sortOrder: number;
  isDefault: 'Y' | 'N';
  isTriage: 'Y' | 'N';
  createdAt: string;
  updatedAt: string;
  deletedAt: string | null;
}

// POST /api/tracker/projectState/getList 的入参（按 projectId 查）。
export interface ProjectStateGetListRequest {
  projectId: number;
}

export class ProjectStateService {
  // getList：返回指定项目下的全部状态。
  static getList(req: ProjectStateGetListRequest): Promise<ProjectStateModel[]> {
    return request<ProjectStateModel[]>('POST', '/api/tracker/projectState/getList', req);
  }
}
