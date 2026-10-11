import { request } from './http';

// 浏览器会话域契约：镜像 Go server/internal/browser/view.go（SessionView/BrowserViewSnapshot/
// Frame 为响应 shape SSOT）与 dal/types/browser_session.go（请求 DTO），手写镜像无生成器。
// navigate/screenshot 的响应是引擎 CallToolResult 原样透传（Content 数组含 ImageContent）。

// 会话状态机（view.go SessionStatus，D4）：idle 已释放（条目驻留供投影）→ starting →
// ready；任一态可 → failed；failed/idle 经 ensure 自动（重）拉。
export type BrowserSessionStatus = 'idle' | 'starting' | 'ready' | 'failed';

// 操作流水结果分类（view.go CallOutcome* 常量取值域；轻量展示分类，不承载重试语义）。
export type BrowserCallOutcome = 'ok' | 'error' | 'timeout' | 'stale_ref' | 'closed';

// 页面投影行（view.go PageView；每次引擎调用成功后刷新，截图轮询豁免）。
export interface BrowserPageView {
  index: number;
  title: string;
  url: string;
  current: boolean;
}

// 轻量操作流水行（view.go RecentCall；环形缓冲上限 20，tool 为引擎实名）。
export interface BrowserRecentCall {
  tool: string;
  issueTag?: string;
  outcome: BrowserCallOutcome;
  error?: string;
}

// 单 profile 会话投影（view.go SessionView；pages/recentCalls 为 omitempty 可选）。
export interface BrowserSessionView {
  profile: string;
  status: BrowserSessionStatus;
  headless: boolean;
  error?: string;
  pages?: BrowserPageView[];
  lastIssueTag?: string;
  recentCalls?: BrowserRecentCall[];
}

// 全量快照（view.go BrowserViewSnapshot；sessions 按 profile 字典序稳定排列，恒非 null）。
export interface BrowserSessionSnapshot {
  sessions: BrowserSessionView[];
}

// SSE 帧型全集（view.go FrameType，D6）：snapshot（建连首帧全量）+ sessions（增量 upsert）。
export type BrowserFrameType = 'snapshot' | 'sessions';

// SSE 帧信封（view.go Frame）：seq 全局单调递增（前端 gap 即重连取快照），type 判别载荷。
export interface BrowserFrame {
  seq: number;
  type: BrowserFrameType;
  snapshot?: BrowserSessionSnapshot;
  sessions?: BrowserSessionView[];
}

// 引擎 CallToolResult 透传形态（go-sdk wire；仅需消费的字段，Content 数组原样保留）。
export interface McpCallToolResult {
  content?: McpContent[];
  isError?: boolean;
}

// 引擎 Content 元素（本轮消费 text/image 两型；其余型别引擎不会在截图/导航结果中出现）。
export type McpContent
  = | { type: 'text'; text?: string }
    | { type: 'image'; data?: string; mimeType?: string };

// screenshotImageOf 从引擎结果提取截图 dataURL（纯函数；无图返回 null——引擎 omit 模式
// 或帧形态变化时调用方回落视口占位态，视口只认「一帧图」，帧源无关）。
export function screenshotImageOf(result: McpCallToolResult | null | undefined): string | null {
  const image = result?.content?.find(item => item.type === 'image');
  if (image?.type !== 'image' || !image.data) {
    return null;
  }
  return `data:${image.mimeType || 'image/png'};base64,${image.data}`;
}

// POST /api/browserSession/ensure：拉起/复用 profile 会话（同步等就绪）。Headless 为
// D10 覆盖：缺省（undefined = 键省略）读 profile 偏好文件，显式传入覆写并回存。
export interface BrowserSessionEnsureRequest {
  profile: string;
  headless?: boolean;
}

// POST /api/browserSession/close：释放会话（幂等，登录态在 profile 目录续存）。
export interface BrowserSessionCloseRequest {
  profile: string;
}

// POST /api/browserSession/navigate：当前页打开地址（引擎结果原样透传）。
export interface BrowserSessionNavigateRequest {
  profile: string;
  url: string;
}

// POST /api/browserSession/screenshot：截当前页（ImageContent base64 + TextContent 落盘
// 路径并存，供面板轮询伪实时）。
export interface BrowserSessionScreenshotRequest {
  profile: string;
}

// POST /api/browserSession/analyze：「AI 分析此页」即时受理（会话缺失/回合进行中即中文
// 报错，不排队）；pageId 缺省 = 当前页。
export interface BrowserSessionAnalyzeRequest {
  profile: string;
  issueId: string;
  pageId?: number;
}

export class BrowserSessionService {
  // getInfo：当前全量会话投影快照（轮询读端，无副作用）。
  static getInfo(): Promise<BrowserSessionSnapshot> {
    return request<BrowserSessionSnapshot>('POST', '/api/browserSession/getInfo', {});
  }

  // ensure：拉起/复用 profile 会话（同步等就绪；headless 语义见请求 DTO 注释）。
  static ensure(req: BrowserSessionEnsureRequest): Promise<BrowserSessionView> {
    return request<BrowserSessionView>('POST', '/api/browserSession/ensure', req);
  }

  // close：释放会话（幂等）。响应 = 释放后的投影行——收尾前服务端已捕获引擎真实当前页
  // （浏览器窗口内手动导航只有引擎知道），无头切换/重开恢复现场以此为准。
  static close(req: BrowserSessionCloseRequest): Promise<BrowserSessionView> {
    return request<BrowserSessionView>('POST', '/api/browserSession/close', req);
  }

  // navigate：当前页打开地址（引擎结果透传；页面列表刷新由后端投影 + SSE 呈现）。
  static navigate(req: BrowserSessionNavigateRequest): Promise<McpCallToolResult> {
    return request<McpCallToolResult>('POST', '/api/browserSession/navigate', req);
  }

  // screenshot：截当前页（面板轮询伪实时的帧源）。
  static screenshot(req: BrowserSessionScreenshotRequest): Promise<McpCallToolResult> {
    return request<McpCallToolResult>('POST', '/api/browserSession/screenshot', req);
  }

  // analyze：「AI 分析此页」即时受理（回答走 issue 绑定 agent 会话既有链路）。
  static analyze(req: BrowserSessionAnalyzeRequest): Promise<null> {
    return request<null>('POST', '/api/browserSession/analyze', req);
  }
}
