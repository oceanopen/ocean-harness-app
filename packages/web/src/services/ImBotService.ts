import { request } from './http';

/**
 * ImBotModel：对齐后端 types.ImBotResponseData 的 JSON 形态（t_im_bots + 运行态合并）。
 * secret 为明文回传（桌面数据全本地，编辑抽屉回显用，前端默认掩码展示）；编辑保存时 secret 留空 = 沿用原值。
 */
export interface ImBotAccessPolicy {
  mode: 'open' | 'allowlist'; // open 全放行 / allowlist 白名单（fail closed）
  allowUsers: string[]; // 企微 userid
}

export type ImBotConversationType = 'workspace' | 'project';

export interface ImBotModel {
  id: string; // uuid
  name: string;
  channel: string; // 渠道枚举（wecom | weixin）
  conversationType: ImBotConversationType; // 会话类型两档（D8）：workspace / project
  workspaceId: string; // workspace 档的关联工作空间（project 档为空）
  projectId: string; // project 档的关联项目
  taskId: string; // project 档可选关联任务（空 = 未关联，收消息拦截提示）
  workspaceName: string; // 关联工作空间名（展示用）
  accessPolicy: ImBotAccessPolicy;
  enabled: boolean;
  secret: string; // 明文（本地数据语义，编辑抽屉回显；前端默认掩码 + 显式切换明文）
  botId: string; // 企微后台颁发；weixin 行为空
  accountId: string; // 微信 iLink 账号 id；wecom 行为空
  connState: string; // connecting | connected | disconnected | stopped
  lastError: string;
  createdAt: string;
  updatedAt: string;
}

// POST /api/imBot/create 的入参（仅 wecom 可手工创建，weixin 由后端拒绝并引导扫码）。
export interface ImBotCreateRequest {
  channel: string;
  name: string;
  botId: string;
  secret: string;
  conversationType: ImBotConversationType;
  workspaceId?: string; // workspace 档必填
  projectId?: string; // project 档必填
  taskId?: string; // project 档可选
  accessPolicy: ImBotAccessPolicy;
  enabled?: boolean;
}

// POST /api/imBot/update 的入参（secret 留空 = 沿用原值；weixin 行后端不触凭据列）。
export interface ImBotUpdateRequest {
  id: string;
  name: string;
  botId: string;
  secret?: string;
  conversationType: ImBotConversationType;
  workspaceId?: string;
  projectId?: string;
  taskId?: string;
  accessPolicy: ImBotAccessPolicy;
  enabled?: boolean;
}

// POST /api/imBot/delete 的入参。
export interface ImBotDeleteRequest {
  id: string;
}

// POST /api/imBot/restart 的入参（以当前配置重连：被踢/挂死后恢复）。
export interface ImBotRestartRequest {
  id: string;
}

/**
 * ImBotConversationModel：对齐后端 types.ImBotConversationData 的 JSON 形态
 * （t_im_bot_conversations 会话行投影）。chatType 由会话键前缀派生，绑定锚为会话级。
 */
export interface ImBotConversationModel {
  conversationKey: string; // single:<userid> / group:<chatid>
  chatType: 'single' | 'group'; // single 私聊 / group 群聊（前端映射文案）
  conversationId: string; // 关联会话实体 id（T4.3 消息路由回写；空 = 尚未有消息）
  lastMessageAt: string | null; // null = 从未活跃
}

// POST /api/imBot/getConversations 的入参。
export interface ImBotGetConversationsRequest {
  botId: string;
}

/**
 * ─── 扫码授权接入（provision）─── 凭据仅在服务端落库（扫码链路不回传，落库后经 getList
 * 明文回显——本地数据语义）；qrContent 为公开的扫码链接（两侧均前端本地渲染成二维码）。
 */

// 扫码会话状态（同后端状态机；scanned/needs_verification 仅 weixin——微信协议区分已扫码
// 与需配对码两中间态）。
export type ImBotProvisionState
  = | 'pending'
    | 'scanned'
    | 'needs_verification'
    | 'connecting'
    | 'connected'
    | 'failed'
    | 'expired'
    | 'cancelled';

// POST /api/imBot/provisionBegin 的入参（weixin 传非空 botId = 对既有 bot 换绑凭据，省略/空 = 新建）。
export interface ImBotProvisionBeginRequest {
  channel: string;
  botId?: string;
}

// POST /api/imBot/provisionPoll 的入参。
export interface ImBotProvisionPollRequest {
  channel: string;
  attemptId: string;
}

// POST /api/imBot/provisionCancel 的入参。
export interface ImBotProvisionCancelRequest {
  channel: string;
  attemptId: string;
}

// POST /api/imBot/provisionVerifyCode 的入参（微信扫码配对码提交，仅 needs_verification 态受理）。
export interface ImBotProvisionVerifyCodeRequest {
  channel: string;
  attemptId: string;
  verifyCode: string;
}

// provisionBegin / provisionPoll 的响应（扫码会话快照）。
export interface ImBotProvisionView {
  attemptId: string;
  state: ImBotProvisionState;
  qrContent?: string; // 二维码扫码链接（两侧均前端本地渲染成二维码）
  expiresAt: number; // 毫秒时间戳（0 = 无 QR 语境）
  pollIntervalMs: number; // 宿主下发轮询节奏（前端不自造）
  errCode?: string;
  botId?: string; // connected 后的 bot 行 id（含换绑目标）
}

export class ImBotService {
  // getList：返回全部 bot（含运行态投影）。
  static getList(): Promise<ImBotModel[]> {
    return request<ImBotModel[]>('POST', '/api/imBot/getList', {});
  }

  // getInfo：返回单个 bot。
  static getInfo(req: { id: string }): Promise<ImBotModel> {
    return request<ImBotModel>('POST', '/api/imBot/getInfo', req);
  }

  // create：新增 bot（后端校验 + 启用即拉起连接），返回新建实体。
  static create(req: ImBotCreateRequest): Promise<ImBotModel> {
    return request<ImBotModel>('POST', '/api/imBot/create', req);
  }

  // update：更新 bot（secret 留空沿用原值；配置变更即热更新连接），返回更新后实体。
  static update(req: ImBotUpdateRequest): Promise<ImBotModel> {
    return request<ImBotModel>('POST', '/api/imBot/update', req);
  }

  // delete：物理删除 bot（级联清会话映射 + 停连接）。
  static delete(req: ImBotDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/imBot/delete', req);
  }

  // restart：以当前落库配置重连。
  static restart(req: ImBotRestartRequest): Promise<void> {
    return request<void>('POST', '/api/imBot/restart', req);
  }

  // getConversations：返回 bot 会话列表（绑定锚 + 活跃时间，最近活跃在前）。
  static getConversations(req: ImBotGetConversationsRequest): Promise<ImBotConversationModel[]> {
    return request<ImBotConversationModel[]>('POST', '/api/imBot/getConversations', req);
  }

  // provisionBegin：开新扫码授权会话（渠道分发；已有会话自动取消；weixin 非空 botId = 换绑）。
  static provisionBegin(req: ImBotProvisionBeginRequest): Promise<ImBotProvisionView> {
    return request<ImBotProvisionView>('POST', '/api/imBot/provisionBegin', req);
  }

  // provisionPoll：轮询扫码结果（connected 即激活完成，botId 为目标 bot 行 id）。
  static provisionPoll(req: ImBotProvisionPollRequest): Promise<ImBotProvisionView> {
    return request<ImBotProvisionView>('POST', '/api/imBot/provisionPoll', req);
  }

  // provisionVerifyCode：提交微信扫码配对码（仅 needs_verification 态受理，wecom 无此环节）。
  static provisionVerifyCode(req: ImBotProvisionVerifyCodeRequest): Promise<void> {
    return request<void>('POST', '/api/imBot/provisionVerifyCode', req);
  }

  // provisionCancel：取消扫码会话。
  static provisionCancel(req: ImBotProvisionCancelRequest): Promise<void> {
    return request<void>('POST', '/api/imBot/provisionCancel', req);
  }
}
