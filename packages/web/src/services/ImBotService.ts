import { request } from './http';

// ImBotModel：对齐后端 types.ImBotResponseData 的 JSON 形态（t_im_bots + 运行态合并）。
// secret 为明文回传（桌面数据全本地，编辑抽屉回显用，前端默认掩码展示）；编辑保存时
// secret 留空 = 沿用原值。
export interface ImBotAccessPolicy {
  mode: 'open' | 'allowlist'; // open 全放行 / allowlist 白名单（fail closed）
  allowUsers: string[]; // 企微 userid
}

export interface ImBotModel {
  id: number;
  name: string;
  channel: string; // 渠道枚举（本期恒 'wecom'，后续 feishu 等扩展）
  workspaceId: number; // 0 = 未选择（扫码接入待补选）
  workspaceName: string; // 关联工作空间名（展示用）
  model: string; // claude --model；空 = CLI 默认
  systemPrompt: string; // 人设（--append-system-prompt）
  allowedTools: string[]; // --allowedTools；空数组 = 后端默认白名单
  accessPolicy: ImBotAccessPolicy;
  enabled: boolean;
  secret: string; // 明文（本地数据语义，编辑抽屉回显；前端默认掩码 + 显式切换明文）
  botId: string;
  connState: string; // connecting | connected | disconnected | stopped
  lastError: string;
  createdAt: string;
  updatedAt: string;
}

// POST /api/imBot/create 的入参。
export interface ImBotCreateRequest {
  name: string;
  botId: string;
  secret: string;
  workspaceId: number; // 必选工作空间（会话目录取其 dir）
  model?: string;
  systemPrompt?: string;
  allowedTools?: string[];
  accessPolicy: ImBotAccessPolicy;
  enabled?: boolean;
}

// POST /api/imBot/update 的入参（secret 留空 = 沿用原值）。
export interface ImBotUpdateRequest {
  id: number;
  name: string;
  botId: string;
  secret?: string;
  workspaceId: number;
  model?: string;
  systemPrompt?: string;
  allowedTools?: string[];
  accessPolicy: ImBotAccessPolicy;
  enabled?: boolean;
}

// POST /api/imBot/delete 的入参。
export interface ImBotDeleteRequest {
  id: number;
}

// POST /api/imBot/restart 的入参（以当前配置重连：被踢/挂死后恢复）。
export interface ImBotRestartRequest {
  id: number;
}

// ─── 扫码授权接入（provision）───
// 凭据安全语义：secret 仅在服务端落库，provision 扫码链路不回传；落库后经 getList/getInfo
// 明文回显（本地数据语义，见 ImBotModel.secret）。qrContent 为腾讯授权页 URL（公开）。

// 扫码会话状态（同后端状态机；无 scanned 中间态——腾讯协议不区分已扫码未确认）。
export type ImBotProvisionState = 'pending' | 'connecting' | 'connected' | 'failed' | 'expired' | 'cancelled';

// POST /api/imBot/provisionBegin 的响应。
export interface ImBotProvisionView {
  attemptId: string;
  state: ImBotProvisionState;
  qrContent?: string; // 授权页 URL（二维码内容，pending 态非空）
  expiresAt: number; // 毫秒时间戳（0 = 无 QR 语境）
  pollIntervalMs: number; // 宿主下发轮询节奏（前端不自造）
  errCode?: string;
  botId?: number; // connected 后的新 bot 行 id
}

// POST /api/imBot/provisionPoll 的入参。
export interface ImBotProvisionPollRequest {
  attemptId: string;
}

// POST /api/imBot/provisionCancel 的入参。
export interface ImBotProvisionCancelRequest {
  attemptId: string;
}

export class ImBotService {
  // getList：返回全部 bot（含运行态投影）。
  static getList(): Promise<ImBotModel[]> {
    return request<ImBotModel[]>('POST', '/api/imBot/getList', {});
  }

  // getInfo：返回单个 bot。
  static getInfo(req: { id: number }): Promise<ImBotModel> {
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

  // provisionBegin：开新扫码授权会话（返回二维码内容 + 轮询节奏；已有会话自动取消）。
  static provisionBegin(): Promise<ImBotProvisionView> {
    return request<ImBotProvisionView>('POST', '/api/imBot/provisionBegin', {});
  }

  // provisionPoll：轮询扫码结果（connected 即激活完成，botId 为新建 bot 行 id）。
  static provisionPoll(req: ImBotProvisionPollRequest): Promise<ImBotProvisionView> {
    return request<ImBotProvisionView>('POST', '/api/imBot/provisionPoll', req);
  }

  // provisionCancel：取消扫码会话。
  static provisionCancel(req: ImBotProvisionCancelRequest): Promise<void> {
    return request<void>('POST', '/api/imBot/provisionCancel', req);
  }
}
