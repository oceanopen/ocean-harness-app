import { request } from './http';

// StateGroup issue 状态分组枚举（对齐后端 enums.StateGroup）。
// 取值 backlog/unstarted/started/completed/cancelled（completed 组触发 issue.completed_at）。
export type StateGroup = 'backlog' | 'unstarted' | 'started' | 'completed' | 'cancelled';

// ─── 第 1+2 层：状态目录常量（GET /api/tracker/projectState/catalog，对齐后端 enums.StateGroupCatalog/StateCatalog）───
// 数据层不国际化：name/色由 Go 直出中文，前端原样展示（i18n 仅用于页面静态文案）。

// 分组展示元数据（列表分组头/分组排序用）。
export interface StateGroupMeta {
  code: StateGroup;
  name: string;
  color: string;
  sortOrder: number;
}

// 子状态定义。开发步骤即 started 组里除「进行中」外的子 state，按 code 匹配（docs/issue.md §8）。
export interface StateMeta {
  groupCode: StateGroup;
  code: string;
  name: string;
  color: string;
  icon: string;
  sortOrder: number;
}

// catalog 接口响应：分组元数据 + 全部子状态（扁平，前端按 groupCode 自行分组）。
export interface CatalogResponse {
  groups: StateGroupMeta[];
  states: StateMeta[];
}

// ─── 第 3 层：projectState 数据（引用模型，docs/issue.md §3）───
// 只存 (stateGroupCode, stateCode)，展示元数据由目录 join 提供（不在数据行冗余）。

// ProjectStateModel：对齐后端 model.ProjectState 的 JSON 形态（getList 返回）。
export interface ProjectStateModel {
  id: number;
  projectId: number;
  workspaceId: number;
  stateGroupCode: StateGroup;
  stateCode: string;
  sortOrder: number;
  isDefault: 'Y' | 'N';
  createdAt: string;
  updatedAt: string;
  deletedAt: string | null;
}

// POST /api/tracker/projectState/getList 的入参（按 projectId 查）。
export interface ProjectStateGetListRequest {
  projectId: number;
}

// 项目状态项（随项目 create/update 全量提交，对应后端 types.ProjectStateItem）。
// (stateGroupCode, stateCode) 引用目录；sortOrder 为展示顺序；isDefault 每项目恰一 Y。
export interface ProjectStateItem {
  stateGroupCode: StateGroup;
  stateCode: string;
  sortOrder: number;
  isDefault: 'Y' | 'N';
}

export class ProjectStateService {
  // getCatalog：返回固定的状态目录常量（第 1+2 层）。全局唯一、与项目无关。
  // 前端用它渲染状态管理模块、状态徽章、步骤条、看板列头。
  static getCatalog(): Promise<CatalogResponse> {
    return request<CatalogResponse>('GET', '/api/tracker/projectState/catalog');
  }

  // getList：返回指定项目下的全部状态（行含 stateCode，不再有 name/color）。
  static getList(req: ProjectStateGetListRequest): Promise<ProjectStateModel[]> {
    return request<ProjectStateModel[]>('POST', '/api/tracker/projectState/getList', req);
  }
}
