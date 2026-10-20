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

// 内置运行时版本信息（对齐 Go RuntimeInfo：随包分发的 vendored 运行时，缺失为空串）。
export interface RuntimeInfo {
  nodeVersion: string; // 内嵌 node 版本（runtime/node/<semver> 目录名）
  claudeVersion: string; // vendored claude --version 输出
}

// getServerRunInfo 返回数据（对齐 Go ServerRunInfoResponseData）。
export interface ServerRunInfoResponseData {
  sysInfo: SysInfo;
  serverInfo: ServerInfo;
  runtimeInfo: RuntimeInfo;
}

// GET /api/baseInfo/getServerRunInfo 的入参（当前无入参，预留扩展）。
export interface ServerRunInfoRequest {}

export class BaseInfoService {
  // GET /api/baseInfo/getServerRunInfo：返回本地 HTTP 服务的系统信息与运行信息。
  static getServerRunInfo(_req?: ServerRunInfoRequest): Promise<ServerRunInfoResponseData> {
    return request<ServerRunInfoResponseData>('GET', '/api/baseInfo/getServerRunInfo');
  }
}
