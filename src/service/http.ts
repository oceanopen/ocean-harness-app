import { commands } from '@src/shared/bindings';

// go-server 统一响应封装（对齐 src-server/internal/apis/response.go）。
export interface ApiResponse<T> {
  code: number; // 0 = 成功
  msg: string;
  data: T;
}

// getServerAddress 取 go-server base URL：调 Rust httpServerStatus（裸 T 命令）。
// runState === 'running' 时 Rust 已探活端口就绪，address 可直接 fetch；否则返回 null。
async function getServerAddress(): Promise<string | null> {
  try {
    const s = await commands.httpServerStatus();
    return s.runState === 'running' ? s.address : null;
  } catch (e) {
    console.warn('[service] httpServerStatus failed:', e);
    return null;
  }
}

// request 统一 fetch：三段式错误处理（服务未运行 / HTTP 状态 / 业务 code）。
// 各 XxxService 直接调用，按 action 语义自行声明 GET/POST 与路径。
export async function request<T>(method: 'GET' | 'POST', path: string, body?: unknown): Promise<T> {
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
