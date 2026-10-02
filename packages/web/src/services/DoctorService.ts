import { request } from './http';

// doctor 域契约：镜像 Go internal/dal/types/doctor.go（T1.4，手写镜像无生成器）。
// SSOT 为 sidecar 进程内结论缓存（重启即 unknown，启动后台探测重填）。

// 单条目四态：healthy/unhealthy 是结论三态；unknown = 从未探测（≠ 失败）；checking =
// 探测在跑（派生态，此时结论字段为上一次结论或零值）。
export type DoctorStatus = 'healthy' | 'unhealthy' | 'unknown' | 'checking';

// 单条目四态视图。unhealthy 时 stage/reason 非空（reason 为后端生成的用户可读文案，
// 前端直显）；healthy 时 commandCount/nodeVersion 有值。
export interface DoctorEntryState {
  agentCode: string;
  status: DoctorStatus;
  checkedAt: string; // 结论时刻（ISO；unknown/checking 且无旧结论时为 Go 零值串）
  stage?: string;
  reason?: string;
  commandCount: number;
  nodeVersion?: string;
}

// POST /api/doctor/check：受理探测（异步，立即返回）。agentCode 缺省 = 全部 enabled 条目。
export interface DoctorCheckRequest {
  agentCode?: string;
}

// /api/doctor/check 响应：accepted 为本次受理的 agentCode 清单（在跑的同样计入），
// entries 为受理时刻快照（可直接落地，后续以 getInfo 轮询跟进）。
export interface DoctorCheckResponseData {
  accepted: string[];
  entries: DoctorEntryState[];
}

// POST /api/doctor/getInfo 的入参（当前无参，预留筛选位）。
export interface DoctorGetInfoRequest {}

export class DoctorService {
  // check：受理探测（单飞：在跑条目 join 不重跑）。
  static check(req: DoctorCheckRequest = {}): Promise<DoctorCheckResponseData> {
    return request<DoctorCheckResponseData>('POST', '/api/doctor/check', req);
  }

  // getInfo：全量 enabled 条目的当前四态快照（轮询读端，无副作用）。
  static getInfo(req: DoctorGetInfoRequest = {}): Promise<DoctorEntryState[]> {
    return request<DoctorEntryState[]>('POST', '/api/doctor/getInfo', req);
  }
}
