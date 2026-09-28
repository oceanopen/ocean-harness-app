import { request } from './http';

// 插件市场域 service：对齐后端 types.PluginMarketplaceListData 的 JSON 形态（手工镜像，无生成器）。
// 后端无本地表——市场注册表与安装状态的 SSOT 恒为 claude 侧，本域全部为实时投影；
// 所有写操作返回最新列表投影，前端一次往返即完成刷新。

// 插件组件构成统计（按官方标准布局扫描插件目录）。
export interface PluginComponentsModel {
  commands: number; // commands/*.md 数量
  skills: number; // skills/<name>/SKILL.md 子目录数（含插件根单 SKILL.md 布局）
  agents: number; // agents/*.md 数量
  hooks: number; // hooks/hooks.json 存在计 1
  mcpServers: number; // .mcp.json 存在计 1
}

// 市场内一个插件条目的投影（市场清单信息 × claude 安装状态 join）。
export interface MarketplacePluginModel {
  name: string;
  source: string; // 相对路径字符串；外部引用为紧凑 JSON
  sourceExternal: boolean; // 外部引用（github/npm 等）无法本地统计组件
  dir: string; // 插件目录绝对路径；外部引用为空
  description: string;
  version: string; // 市场清单版本（plugin.json 优先）
  components: PluginComponentsModel;
  installed: boolean;
  enabled: boolean;
  scope: string; // join 命中的安装 scope（user/project/local）；未安装为空（非 user scope 行禁用操作）
  installedVersion: string; // claude 侧记录的已安装版本
}

// 一个插件市场的投影。
export interface PluginMarketplaceModel {
  name: string;
  sourceType: string; // local | github | git | 其他 CLI 原始值
  sourceDetail: string; // 本地路径 / owner/repo / git URL
  installLocation: string; // claude 侧市场根目录
  description: string;
  version: string;
  supportedClis: string[]; // 清单目录探测（v1：["claude"]）
  plugins: MarketplacePluginModel[];
  scanError: string; // 清单扫描失败原因；空 = 正常
}

// getList 与全部写操作的统一响应（clis：本域支持安装操作的 CLI 清单）。
export interface PluginMarketplaceListModel {
  clis: string[];
  marketplaces: PluginMarketplaceModel[];
}

// POST /api/pluginMarketplace/getList 的入参（无筛选）。
export type PluginMarketplaceGetListRequest = Record<string, never>;

// POST /api/pluginMarketplace/add 的入参（本地绝对路径 / GitHub owner/repo / git URL）。
export interface PluginMarketplaceAddRequest {
  source: string;
}

// POST /api/pluginMarketplace/remove 的入参（移除会连带卸载该市场全部已装插件）。
export interface PluginMarketplaceRemoveRequest {
  name: string;
}

// POST /api/pluginMarketplace/update 的入参（刷新市场）。
export interface PluginMarketplaceUpdateRequest {
  name: string;
}

// /api/plugin/<action> 的操作目标（cli 由本 service 注入，v1 恒 claude；
// 多 CLI 扩展时前端按市场 supportedClis 分流传入）。
export interface PluginOperationRequest {
  name: string;
  marketplace: string;
}

// v1 唯一支持安装操作的开发工具；接口契约已带 cli 维度（后端按值分发）。
const PLUGIN_CLI = 'claude';

export class PluginMarketplaceService {
  // getList：全部插件市场投影（市场注册表 × 清单扫描 × 安装状态）。
  static getList(): Promise<PluginMarketplaceListModel> {
    return request<PluginMarketplaceListModel>('POST', '/api/pluginMarketplace/getList', {});
  }

  // add：注册插件市场，返回最新列表投影。
  static add(req: PluginMarketplaceAddRequest): Promise<PluginMarketplaceListModel> {
    return request<PluginMarketplaceListModel>('POST', '/api/pluginMarketplace/add', req);
  }

  // update：刷新插件市场，返回最新列表投影。
  static update(req: PluginMarketplaceUpdateRequest): Promise<PluginMarketplaceListModel> {
    return request<PluginMarketplaceListModel>('POST', '/api/pluginMarketplace/update', req);
  }

  // remove：注销插件市场（连带卸载其全部已装插件），返回最新列表投影。
  static remove(req: PluginMarketplaceRemoveRequest): Promise<PluginMarketplaceListModel> {
    return request<PluginMarketplaceListModel>('POST', '/api/pluginMarketplace/remove', req);
  }

  // install：安装插件（user scope 全局，与命令行默认一致）。
  static install(req: PluginOperationRequest): Promise<PluginMarketplaceListModel> {
    return request<PluginMarketplaceListModel>('POST', '/api/plugin/install', { cli: PLUGIN_CLI, ...req });
  }

  // uninstall：卸载插件。
  static uninstall(req: PluginOperationRequest): Promise<PluginMarketplaceListModel> {
    return request<PluginMarketplaceListModel>('POST', '/api/plugin/uninstall', { cli: PLUGIN_CLI, ...req });
  }

  // enable：启用插件。
  static enable(req: PluginOperationRequest): Promise<PluginMarketplaceListModel> {
    return request<PluginMarketplaceListModel>('POST', '/api/plugin/enable', { cli: PLUGIN_CLI, ...req });
  }

  // disable：禁用插件（保留安装）。
  static disable(req: PluginOperationRequest): Promise<PluginMarketplaceListModel> {
    return request<PluginMarketplaceListModel>('POST', '/api/plugin/disable', { cli: PLUGIN_CLI, ...req });
  }

  // updatePlugin：更新插件到市场最新版本。
  static updatePlugin(req: PluginOperationRequest): Promise<PluginMarketplaceListModel> {
    return request<PluginMarketplaceListModel>('POST', '/api/plugin/update', { cli: PLUGIN_CLI, ...req });
  }
}
