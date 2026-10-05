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
  source?: 'panel' | 'bot'; // user 条目来源（view.go EntrySource*；空 = 无来源标记）
  display?: AcpEntryDisplay; // user 条目展示元数据（缺失回落 text 直显，见 AcpEntryDisplay）
  toolCall?: AcpToolCallView;
}

// user 条目展示元数据（view.go EntryDisplay，显示-发送文本分离）：text 为正文原文
// （区别于条目 text 的围栏全文，围栏只归模型侧），quote / files 为 IM 引用与附件的
// 展示投影；display 缺失（旧 sidecar + 新前端混跑窗口，历史条目随 sidecar 重启即消失）
// 前端回落条目 text 原样直显。
export interface AcpEntryDisplay {
  text?: string;
  quote?: AcpEntryQuote;
  files?: AcpEntryFile[];
}

// IM 引用块展示元数据（view.go EntryQuote）。
export interface AcpEntryQuote {
  author?: string;
  text?: string;
  truncated?: boolean;
}

// IM 附件展示元数据（view.go EntryFile）。
export interface AcpEntryFile {
  name?: string;
  path?: string;
}

// 权限选项 kind（ACP wire schema.PermissionOptionKind 透传取值域）：决定审批按钮的
// 色彩与轻重（allow 绿/reject 红、*_always 实心=附带「记住」语义）。
export type AcpPermissionOptionKind = 'allow_once' | 'allow_always' | 'reject_once' | 'reject_always';

// 权限选项（view.go PendingView.Options 条目，schema.PermissionOption 透传）。
export interface AcpPermissionOption {
  optionId: string;
  name: string;
  kind: AcpPermissionOptionKind;
}

// elicitation 全保真 wire（view.go PendingView.Request，acp.ElicitationWire 透传）。
// acp-go 生成模型把 spec 的 requestedSchema/url 丢弃（上游生成缺陷），后端在入站链
// 拦截原始参数自行解码补齐（server/internal/acp/elicitation.go），此结构即其投影。
export interface AcpElicitationWire {
  mode?: string; // form | url | 未知形态（前端按 form 表单 / message+跳过 兜底二分）
  message?: string;
  sessionId?: string;
  toolCallId?: string;
  requestId?: string;
  requestedSchema?: unknown; // 表单 JSON Schema（适配器子集，渲染计划见 elicitationForm.ts）
  url?: string;
  elicitationId?: string;
}

// 挂起交互投影（view.go PendingView）：字段按 kind 取舍——permission 用 options/toolCall，
// elicitation 用 request。
export interface AcpPendingView {
  pendingId: number;
  kind: 'permission' | 'elicitation';
  options?: AcpPermissionOption[];
  toolCall?: AcpToolCallView;
  request?: AcpElicitationWire;
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
// pickedLaunchMode 临场启动声明（LaunchModePicker 三选一受理，仅本次会话有效不落库；
// AcpSessionView 即 acp 意图载体，视图内受理一律显式声明）。
export interface AcpSessionEnsureRequest {
  issueId: string;
  pickedLaunchMode?: 'acp';
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

// POST /api/acpSession/respondPermission：应答挂起权限审批。
export interface AcpSessionRespondPermissionRequest {
  issueId: string;
  pendingId: number;
  optionId: string;
}

// POST /api/acpSession/respondElicitation：应答挂起 elicitation（action 三态；content 仅
// accept 时有意义，键值对透传 ACP wire）。
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
