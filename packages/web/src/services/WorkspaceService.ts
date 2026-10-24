import type { AgentCode } from '@src/shared/agentCode';
import { request } from './http';

/**
 * WorkspaceLaunchSettings：启动设置（launch_settings JSON 列的结构化形态）。
 * undefined/空对象 = 未配置 → 消费端回落全局 key。
 */
export interface WorkspaceLaunchSettings {
  mode?: 'none' | 'terminal-auto' | 'acp'; // 未配置等同 none（就绪后出启动方式选择面板）
  agentCode?: AgentCode; // ACP 会话 agent
  autoCommand?: string; // 终端启动 Agent（仅 terminal-auto 消费：主终端直接启动；一期 claude）
}

export interface WorkspaceModel {
  id: string; // uuid
  name: string;
  dir: string;
  description: string;
  emoji: string; // 展示图标（空串 = 前端图标兜底）
  launchSettings?: WorkspaceLaunchSettings; // undefined = 未配置（回落全局）
  createdAt: string;
  updatedAt: string;
}

// POST /api/workspace/getList 的入参（当前无参，预留筛选位）。
export interface WorkspaceGetListRequest {}

// POST /api/workspace/create 的入参。
export interface WorkspaceCreateRequest {
  name: string;
  dir: string;
  description?: string;
  emoji?: string; // 省略 = 前端图标兜底
  launchSettings?: WorkspaceLaunchSettings; // 省略 = 不配置（回落全局）
}

// POST /api/workspace/update 的入参。
export interface WorkspaceUpdateRequest {
  id: string;
  name: string;
  dir: string;
  description?: string;
  emoji?: string;
  launchSettings?: WorkspaceLaunchSettings; // 省略 = 清空配置（回落全局）
}

// POST /api/workspace/delete 的入参。
export interface WorkspaceDeleteRequest {
  id: string;
}

export class WorkspaceService {
  // getList：返回全部工作空间。
  static getList(req: WorkspaceGetListRequest = {}): Promise<WorkspaceModel[]> {
    return request<WorkspaceModel[]>('POST', '/api/workspace/getList', req);
  }

  // create：创建工作空间（恢复式 upsert），返回新建实体。
  static create(req: WorkspaceCreateRequest): Promise<WorkspaceModel> {
    return request<WorkspaceModel>('POST', '/api/workspace/create', req);
  }

  // update：更新工作空间，返回更新后的实体。
  static update(req: WorkspaceUpdateRequest): Promise<WorkspaceModel> {
    return request<WorkspaceModel>('POST', '/api/workspace/update', req);
  }

  // delete：删除工作空间。
  static delete(req: WorkspaceDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/workspace/delete', req);
  }
}
