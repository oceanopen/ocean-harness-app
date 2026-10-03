import { request } from './http';

// ACP 会话域契约：镜像 Go server/internal/acpsession/view.go（ViewSnapshot 为响应 shape
// SSOT）、hub.go（SSE 帧协议）与 dal/types/acp_session.go（请求 DTO），手写镜像无生成器。
// 嵌套 ACP wire 结构原样透传不转译（view.go 注释：镜像只会双份维护），本期不渲染的
// 结构以窄别名/unknown 保快照完整性，后续任务按渲染需要细化。

// 会话状态机（view.go SessionStatus）。
export type AcpSessionStatus = 'idle' | 'starting' | 'ready' | 'failed' | 'terminated';

// 会话记录条目 kind（view.go entryKind* 常量取值域）。
export type AcpConversationEntryKind = 'user' | 'agentMessage' | 'agentThought' | 'toolCall';

// 工具调用状态（ACP wire schema.ToolCallStatus 透传取值域）。
export type AcpToolCallStatus = 'pending' | 'in_progress' | 'completed' | 'failed';

// agent 可切换的会话模式（ACP wire schema.SessionMode 透传）。
export interface AcpSessionMode {
  id: string;
  name: string;
  description?: string;
}

// 本期不渲染的快照类 wire 结构：窄别名透传（reducer 保字段最新替换，UI 消费留后续任务）。
// TODO(T1.7+)：plan 面板 / usage 指示 / 斜杠命令补全按 ACP wire 细化这三类结构。
export type AcpWirePlan = Record<string, unknown>;
export type AcpWireUsage = Record<string, unknown>;
export type AcpWireAvailableCommand = Record<string, unknown>;

// 工具调用条目（view.go ToolCallView）。content/locations 本期不渲染，unknown 透传。
export interface AcpToolCallView {
  toolCallId: string;
  title?: string;
  kind?: string;
  status?: AcpToolCallStatus;
  name?: string;
  content?: unknown[];
  locations?: unknown[];
  rawInput?: unknown;
  rawOutput?: unknown;
}

// 会话记录条目（view.go ConversationEntry）：文本类四类聚合同条目，工具调用按
// toolCallId 覆写式 upsert，条目级整体覆写（SSE entry 帧即整条替换）。
export interface AcpConversationEntry {
  entryId: string;
  kind: AcpConversationEntryKind;
  text?: string;
  toolCall?: AcpToolCallView;
}

// 挂起交互投影（view.go PendingView）。options/toolCall/request 的 wire 明细本期只读
// 提示条不消费，unknown 透传；TODO(T1.7)：审批弹窗/表单面板按 ACP wire 细化。
export interface AcpPendingView {
  pendingId: number;
  kind: 'permission' | 'elicitation';
  options?: unknown[];
  toolCall?: unknown;
  request?: unknown;
}

// 会话视图快照——/api/acpSession 响应与 SSE snapshot 帧的 shape（view.go ViewSnapshot）。
export interface AcpViewSnapshot {
  status: AcpSessionStatus;
  agentCode?: string;
  acpSessionId?: string;
  permissionMode?: string;
  currentModeId?: string;
  availableModes?: AcpSessionMode[];
  entries: AcpConversationEntry[];
  pendings: AcpPendingView[];
  plan?: AcpWirePlan;
  usage?: AcpWireUsage;
  availableCommands?: AcpWireAvailableCommand[];
  turnActive: boolean;
  stopReason?: string;
  error?: string;
}

// SSE 帧型全集（hub.go FrameType）。
export type AcpFrameType
  = | 'snapshot'
    | 'sessionStatus'
    | 'entry'
    | 'planUpdated'
    | 'usageUpdated'
    | 'commandsUpdated'
    | 'modeUpdated'
    | 'pendingOpened'
    | 'pendingClosed'
    | 'turnStarted'
    | 'turnEnded'
    | 'terminated';

// SSE 帧信封（hub.go Frame）：seq 按 issue 单调递增（前端 gap 即重连取快照），type 判别
// 载荷（至多一个非零载荷字段，与 type 对应）。
export interface AcpFrame {
  seq: number;
  issueId: string;
  type: AcpFrameType;
  snapshot?: AcpViewSnapshot;
  status?: AcpSessionFrameStatus;
  entry?: AcpConversationEntry;
  plan?: AcpWirePlan;
  usage?: AcpWireUsage;
  commands?: AcpWireAvailableCommand[];
  mode?: { currentModeId?: string; availableModes?: AcpSessionMode[] };
  pending?: AcpPendingView;
  pendingId?: number;
  turn?: { active: boolean; stopReason?: string; error?: string };
}

// sessionStatus / terminated 帧载荷（hub.go StatusPayload）。
export interface AcpSessionFrameStatus {
  status: AcpSessionStatus;
  agentCode?: string;
  acpSessionId?: string;
  error?: string;
}

// POST /api/acpSession/ensure：幂等受理会话创建（异步受理，立即返回受理时刻快照）。
export interface AcpSessionEnsureRequest {
  issueId: string;
}

// POST /api/acpSession/prompt：受理一轮回合（立即返回，终态经 SSE turnEnded）。
export interface AcpSessionPromptRequest {
  issueId: string;
  text: string;
}

// POST /api/acpSession/cancel：软取消当前回合（幂等）。
export interface AcpSessionCancelRequest {
  issueId: string;
}

// POST /api/acpSession/respondPermission：应答挂起权限审批（UI 交互 TODO(T1.7)）。
export interface AcpSessionRespondPermissionRequest {
  issueId: string;
  pendingId: number;
  optionId: string;
}

// POST /api/acpSession/respondElicitation：应答挂起 elicitation（action 三态；content 仅
// accept 时有意义，键值对透传 ACP wire。UI 交互 TODO(T1.7)）。
export interface AcpSessionRespondElicitationRequest {
  issueId: string;
  pendingId: number;
  action: 'accept' | 'decline' | 'cancel';
  content?: Record<string, unknown>;
}

export class AcpSessionService {
  // ensure：幂等受理（starting/ready join 返现状，failed/terminated 删旧重建）。
  static ensure(req: AcpSessionEnsureRequest): Promise<AcpViewSnapshot> {
    return request<AcpViewSnapshot>('POST', '/api/acpSession/ensure', req);
  }

  // getInfo：当前会话视图快照（轮询读端，无副作用）。
  static getInfo(req: AcpSessionEnsureRequest): Promise<AcpViewSnapshot> {
    return request<AcpViewSnapshot>('POST', '/api/acpSession/getInfo', req);
  }

  // prompt：受理一轮回合（活动回合中被后端拒绝，前端以 turnActive 禁用输入为主）。
  static prompt(req: AcpSessionPromptRequest): Promise<null> {
    return request<null>('POST', '/api/acpSession/prompt', req);
  }

  // cancel：软取消当前回合（幂等）。
  static cancel(req: AcpSessionCancelRequest): Promise<null> {
    return request<null>('POST', '/api/acpSession/cancel', req);
  }

  // respondPermission：应答挂起权限审批。
  static respondPermission(req: AcpSessionRespondPermissionRequest): Promise<null> {
    return request<null>('POST', '/api/acpSession/respondPermission', req);
  }

  // respondElicitation：应答挂起 elicitation。
  static respondElicitation(req: AcpSessionRespondElicitationRequest): Promise<null> {
    return request<null>('POST', '/api/acpSession/respondElicitation', req);
  }
}
