import { request } from './http';

/**
 * ConversationModel：会话实体（应用只存元数据，消息内容直读 claude 本地 jsonl）。
 * claudeSessionId 为终端/ACP 统一锚（空 = 未锚定）；parentId 空 = 主会话。
 */
export interface ConversationModel {
  id: string; // uuid v7
  ownerType: 'workspace' | 'task';
  ownerId: string;
  title: string;
  mode: 'terminal' | 'acp';
  agentCode: string; // 当前恒 claude（多 agent 预留）
  claudeSessionId: string;
  parentId: string;
  createdAt: string;
  updatedAt: string;
}

// POST /api/conversation/getList 的入参（按归属实体过滤，扁平全量）。
export interface ConversationGetListRequest {
  ownerType: 'workspace' | 'task';
  ownerId: string;
}

// POST /api/conversation/getInfo 的入参。
export interface ConversationGetInfoRequest {
  id: string;
}

// POST /api/conversation/create 的入参：mode 由调用方按归属实体 mergeLaunchSettings 算好传入。
export interface ConversationCreateRequest {
  ownerType: 'workspace' | 'task';
  ownerId: string;
  title?: string; // 空 = 默认「新会话」
  mode: 'terminal' | 'acp';
  parentId?: string; // 空 = 主会话
}

// POST /api/conversation/update 的入参（仅改标题）。
export interface ConversationUpdateRequest {
  id: string;
  title: string;
}

// POST /api/conversation/setClaudeSessionId 的入参（终端捕获回写统一锚）。
export interface ConversationSetClaudeSessionIdRequest {
  id: string;
  claudeSessionId: string;
}

// POST /api/conversation/delete 的入参。
export interface ConversationDeleteRequest {
  id: string;
}

export class ConversationService {
  // getList：指定归属实体下的全部会话（created_at 升序）。
  static getList(req: ConversationGetListRequest): Promise<ConversationModel[]> {
    return request<ConversationModel[]>('POST', '/api/conversation/getList', req);
  }

  // getInfo：按 id 取单个会话。
  static getInfo(req: ConversationGetInfoRequest): Promise<ConversationModel> {
    return request<ConversationModel>('POST', '/api/conversation/getInfo', req);
  }

  // create：新建会话。
  static create(req: ConversationCreateRequest): Promise<ConversationModel> {
    return request<ConversationModel>('POST', '/api/conversation/create', req);
  }

  // update：改标题。
  static update(req: ConversationUpdateRequest): Promise<ConversationModel> {
    return request<ConversationModel>('POST', '/api/conversation/update', req);
  }

  // setClaudeSessionId：终端捕获回写统一锚（锚非空不覆盖，返回现行行）。
  static setClaudeSessionId(req: ConversationSetClaudeSessionIdRequest): Promise<ConversationModel> {
    return request<ConversationModel>('POST', '/api/conversation/setClaudeSessionId', req);
  }

  // delete：删除会话行（不动 claude 本地文件）。
  static delete(req: ConversationDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/conversation/delete', req);
  }
}
