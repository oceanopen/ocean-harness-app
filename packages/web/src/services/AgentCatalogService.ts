import type { AgentCode } from '@src/shared/agentCode';
import { request } from './http';

// AgentCatalogEntry：agent catalog 条目投影（/api/agentCatalog/getList）。
export interface AgentCatalogEntry {
  id: string; // 条目 id
  code: AgentCode; // agentCode
  label: string; // 展示名
  version: string; // adapter pin 版本
  description: string;
  strategy: 'npx-adapter' | 'native-acp' | 'in-process-go';
  enabled: boolean; // 仅 enabled 条目进下拉可选面
  nodeMinVersion: string; // node 主版本下限（T1.4 doctor 消费；空 = 不校验）
}

// POST /api/agentCatalog/getList 的入参（当前无参，预留筛选位）。
export interface AgentCatalogGetListRequest {}

export class AgentCatalogService {
  // getList：返回全部 catalog 条目（含 disabled，消费方按 enabled 过滤可选面）。
  static getList(req: AgentCatalogGetListRequest = {}): Promise<AgentCatalogEntry[]> {
    return request<AgentCatalogEntry[]>('POST', '/api/agentCatalog/getList', req);
  }
}
