import { commands } from '@src/shared/bindings';
import { EVENT_HTTP_SERVER_STATE_CHANGED } from '@src/shared/events';
import { listen } from '@tauri-apps/api/event';

// go-server 统一响应封装（对齐 src-server/internal/apis/response.go）。
export interface ApiResponse<T> {
  code: number; // 0 = 成功
  msg: string;
  data: T;
}

// base URL 缓存：避免每次 request 都调 commands.httpServerStatus()（一次 IPC 开销）。
// 服务启动后端口稳定，仅在 EVENT_HTTP_SERVER_STATE_CHANGED（重启/端口变更）时失效重拉。
// undefined = 未加载；string|null = 已加载（null 表示服务未运行）。
let cachedBase: string | null | undefined;
let listenerInitialized = false;

// 一次性订阅服务状态变化：窗口生命周期内常驻，状态变更时清缓存。
// listen 失败（窗口 ACL 拒绝）时 fallback 到每次 IPC，行为退化为现状。
function ensureStateListener() {
  if (listenerInitialized) {
    return;
  }
  listenerInitialized = true;
  listen(EVENT_HTTP_SERVER_STATE_CHANGED, () => {
    cachedBase = undefined;
  }).catch(err => console.warn('[service] listen http-server:state-changed failed:', err));
}

// getServerAddress 取 go-server base URL：优先用缓存，未缓存时调 Rust httpServerStatus（裸 T 命令）。
// runState === 'running' 时 Rust 已探活端口就绪，address 可直接 fetch；否则返回 null。
async function getServerAddress(): Promise<string | null> {
  if (cachedBase !== undefined) {
    return cachedBase;
  }
  try {
    const s = await commands.httpServerStatus();
    cachedBase = s.runState === 'running' ? s.address : null;
  } catch (e) {
    console.warn('[service] httpServerStatus failed:', e);
    cachedBase = null;
  }
  return cachedBase;
}

// request 统一 fetch：三段式错误处理（服务未运行 / HTTP 状态 / 业务 code）。
// 各 XxxService 直接调用，按 action 语义自行声明 GET/POST 与路径。
export async function request<T>(method: 'GET' | 'POST', path: string, body?: unknown): Promise<T> {
  ensureStateListener();
  const base = await getServerAddress();
  if (!base) {
    throw new Error('go-server 未运行');
  }
  const resp = await fetch(`${base}${path}`, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!resp.ok) {
    throw new Error(`HTTP ${resp.status}`);
  }
  const json: ApiResponse<T> = await resp.json();
  if (json.code !== 0) {
    throw new Error(json.msg || `code ${json.code}`);
  }
  return json.data;
}
