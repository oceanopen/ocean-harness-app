// BaseInfoService：/api/baseInfo 命名空间下的接口封装（本地服务系统信息）。
// 类型对齐 src-server/internal/dal/types/base_info.go。

import { request } from './http';

// 系统信息（对齐 Go SysInfo：主机名 / Go 运行时 / OS / 架构）。
export interface SysInfo {
  hostname: string;
  goVersion: string;
  os: string;
  arch: string;
}

// 服务运行信息（对齐 Go ServerInfo：运行模式 / 监听地址 / 日志与数据目录）。
export interface ServerInfo {
  mode: string;
  address: string; // 服务监听地址（http://127.0.0.1:<port>）
  logDir: string; // 日志目录（绝对路径）
  sqliteDir: string; // sqlite 数据目录（绝对路径）
}

// getServerRunInfo 返回数据（对齐 Go ServerRunInfoResponseData）。
export interface ServerRunInfoResponseData {
  sysInfo: SysInfo;
  serverInfo: ServerInfo;
}

// GET /api/baseInfo/getServerRunInfo 的入参（当前无入参，预留扩展）。
export interface ServerRunInfoRequest {}

export class BaseInfoService {
  // GET /api/baseInfo/getServerRunInfo：返回本地 HTTP 服务的系统信息与运行信息。
  static getServerRunInfo(_req?: ServerRunInfoRequest): Promise<ServerRunInfoResponseData> {
    return request<ServerRunInfoResponseData>('GET', '/api/baseInfo/getServerRunInfo');
  }
}
