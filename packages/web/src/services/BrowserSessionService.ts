import { request } from './http';

/**
 * 浏览器会话域契约：镜像 Go server/internal/browser/view.go（SessionView/BrowserViewSnapshot/Frame
 * 为响应 shape SSOT）、browser/live.go（LiveEvent 帧流事件）与 dal/types/browser_session.go（请求 DTO），
 * 手写镜像无生成器。navigate 的响应是引擎 CallToolResult 原样透传（Content 数组原样保留）。
 */

/**
 * 会话状态机（view.go SessionStatus，D4）：idle 已释放（条目驻留供投影）→ starting → ready；
 * 任一态可 → failed；failed/idle 经 ensure 自动（重）拉。
 */
export type BrowserSessionStatus = 'idle' | 'starting' | 'ready' | 'failed';

// 操作流水结果分类（view.go CallOutcome* 常量取值域；轻量展示分类，不承载重试语义）。
export type BrowserCallOutcome = 'ok' | 'error' | 'timeout' | 'stale_ref' | 'closed';

/*
 * 页面投影行（view.go PageView；页面观察者事件驱动维护，current = 浏览器实际激活 tab）。
 * targetId 为 CDP 稳定锚（订阅/点选/分析寻址均用之）；Closed 行为关闭留档（历史可查），
 * Close 收尾捕获行无 targetId（仅供恢复现场取 URL）。
 */
export interface BrowserPageView {
  index: number;
  targetId?: string;
  title: string;
  url: string;
  current: boolean;
  closed?: boolean;
  openedAtMs?: number;
  lastActiveAtMs?: number;
  actions?: BrowserRecentCall[];
}

// 单页操作流水行（view.go RecentCall；per-page 环形上限 30，tool 为引擎实名或输入动作名）。
export interface BrowserRecentCall {
  tool: string;
  issueTag?: string;
  outcome: BrowserCallOutcome;
  error?: string;
  atMs: number;
  targetId?: string;
}

// 单 profile 会话投影（view.go SessionView；pages 为 omitempty 可选）。
export interface BrowserSessionView {
  profile: string;
  status: BrowserSessionStatus;
  headless: boolean;
  error?: string;
  pages?: BrowserPageView[];
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

/**
 * 引擎 Content 元素（CallToolResult wire 全型别的本域消费面；image 型随截图端点退役仅为镜像完备保留）。
 */
export type McpContent
  = | { type: 'text'; text?: string }
    | { type: 'image'; data?: string; mimeType?: string };

/**
 * GET /api/browserSession/stream SSE 事件信封（镜像 browser.LiveEvent wire 形态，type 判别载荷）：
 * frame = jpeg base64 + screencast metadata（初始帧无 metadata，渲染侧按位图尺寸兜底）；target = 投送目标回显（closed 置真后端随补 closed 终态）；
 * error = 自然断连可重订自愈；closed/reject = 终态（reason/真实拒绝原因），前端停止重订。
 */
export type BrowserLiveEventType = 'frame' | 'target' | 'error' | 'closed' | 'reject';

/**
 * screencast metadata（镜像 browser.LiveFrameMeta；deviceWidth/Height 是前端坐标换算基准——渲染尺寸与投送坐标同源）。
 */
export interface BrowserLiveFrameMeta {
  pageScaleFactor: number;
  deviceWidth: number;
  deviceHeight: number;
  scrollOffsetX: number;
  scrollOffsetY: number;
  offsetTop: number;
}

export interface BrowserLiveEvent {
  type: BrowserLiveEventType;
  data?: string;
  metadata?: BrowserLiveFrameMeta;
  url?: string;
  title?: string;
  closed?: boolean;
  message?: string;
  reason?: string;
}

/**
 * POST /api/browserSession/ensure：拉起/复用 profile 会话（同步等就绪）。Headless 为 D10 覆盖：
 * 缺省（undefined = 键省略）读 profile 偏好文件，显式传入覆写并回存。workspaceId 为共享
 * 登录态键：非空写入 profile 偏好持久化（后续冷启动/重建自动读回），缺省读偏好。
 */
export interface BrowserSessionEnsureRequest {
  profile: string;
  headless?: boolean;
  workspaceId?: string;
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

/**
 * POST /api/browserSession/input：远程桌面输入分发（kind→CDP Input 域映射归后端；字段镜像
 * dal/types BrowserInputRequest，可选字段缺省即不传——后端按 kind 白名单校验）。
 * key 域 vk = Windows 虚拟键码（CDP windowsVirtualKeyCode）。
 */
export interface BrowserSessionInputRequest {
  profile: string;
  kind: 'mouse' | 'wheel' | 'key' | 'text';
  action?: string;
  x?: number;
  y?: number;
  button?: string;
  clickCount?: number;
  modifiers?: number;
  deltaX?: number;
  deltaY?: number;
  key?: string;
  code?: string;
  vk?: number;
  text?: string;
}

/**
 * POST /api/browserSession/analyze：「AI 分析此页」即时受理（会话缺失/回合进行中即中文报错，不排队）；
 * targetId 缺省 = 引擎当前页。
 */
export interface BrowserSessionAnalyzeRequest {
  profile: string;
  issueId: string;
  targetId?: string;
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

  /**
   * close：释放会话（幂等）。响应 = 释放后的投影行——收尾前服务端已捕获引擎真实当前页
   * （浏览器窗口内手动导航只有引擎知道），无头切换/重开恢复现场以此为准。
   */
  static close(req: BrowserSessionCloseRequest): Promise<BrowserSessionView> {
    return request<BrowserSessionView>('POST', '/api/browserSession/close', req);
  }

  // navigate：当前页打开地址（引擎结果透传；页面列表刷新由后端投影 + SSE 呈现）。
  static navigate(req: BrowserSessionNavigateRequest): Promise<McpCallToolResult> {
    return request<McpCallToolResult>('POST', '/api/browserSession/navigate', req);
  }

  /**
   * input：远程桌面输入分发（要求活跃观察面——未建实时画面即中文报错；分发失败为
   * 尽力语义，调用方静默，自愈重连后恢复）。
   */
  static input(req: BrowserSessionInputRequest): Promise<null> {
    return request<null>('POST', '/api/browserSession/input', req);
  }

  // analyze：「AI 分析此页」即时受理（回答走 issue 绑定 agent 会话既有链路）。
  static analyze(req: BrowserSessionAnalyzeRequest): Promise<null> {
    return request<null>('POST', '/api/browserSession/analyze', req);
  }
}
