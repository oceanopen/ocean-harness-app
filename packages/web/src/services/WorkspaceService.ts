import { request } from './http';

// WorkspaceLaunchSettings：启动设置（launch_settings JSON 列的结构化形态）。
// 取值域一期由表单枚举约定；undefined/空对象 = 未配置 → 消费端回落全局 key。
export interface WorkspaceLaunchSettings {
  mode?: 'none' | 'terminal-manual' | 'terminal-auto' | 'acp'; // 未配置等同 none（就绪后出启动方式选择面板）
  agentCode?: 'claude-acp' | 'codex' | 'opencode' | 'pi'; // ACP 会话 agent 枚举码（T1.2 catalog 落地后以此为准）
  autoCommand?: string; // 终端启动 Agent（仅 terminal-auto 消费：主终端直接启动；manual 档经终端工具条自行选择；一期 claude）
  permissionMode?: 'acceptEdits' | 'bypassPermissions'; // ACP 执行模式
}

export interface WorkspaceModel {
  id: number;
  name: string;
  dir: string;
  description: string;
  launchSettings?: WorkspaceLaunchSettings; // undefined = 未配置（回落全局）
  createdAt: string;
  updatedAt: string;
}

// POST /api/tracker/workspace/getList 的入参（当前无参，预留筛选位）。
export interface WorkspaceGetListRequest {}

// POST /api/tracker/workspace/create 的入参。
export interface WorkspaceCreateRequest {
  name: string;
  dir: string;
  description?: string;
  launchSettings?: WorkspaceLaunchSettings; // 省略 = 不配置（回落全局）
}

// POST /api/tracker/workspace/update 的入参。
export interface WorkspaceUpdateRequest {
  id: number;
  name: string;
  dir: string;
  description?: string;
  launchSettings?: WorkspaceLaunchSettings; // 省略 = 清空配置（回落全局）
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
