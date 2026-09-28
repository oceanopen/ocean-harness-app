package types

// 插件市场域 DTO：无本地表，全部字段为「claude CLI 输出 × 市场清单扫描」的实时投影。
// 手写镜像前端 PluginMarketplaceService 的 TS interface（项目约定，无生成器）。

// PluginMarketplaceGetListRequest 是 POST /api/pluginMarketplace/getList 的入参（无筛选）。
type PluginMarketplaceGetListRequest struct{}

// PluginMarketplaceAddRequest 是 POST /api/pluginMarketplace/add 的入参。
// source 为 claude CLI 认可的三种形式：本地绝对路径 / GitHub owner/repo / git URL。
type PluginMarketplaceAddRequest struct {
	Source string `json:"source" binding:"required"`
}

// PluginMarketplaceRemoveRequest 是 POST /api/pluginMarketplace/remove 的入参。
// claude 语义：移除会连带卸载该市场已安装的全部插件（前端确认弹窗已告知）。
type PluginMarketplaceRemoveRequest struct {
	Name string `json:"name" binding:"required"`
}

// PluginMarketplaceUpdateRequest 是 POST /api/pluginMarketplace/update 的入参（刷新市场）。
type PluginMarketplaceUpdateRequest struct {
	Name string `json:"name" binding:"required"`
}

// PluginOperationRequest 是 /api/plugin/<action>（install/uninstall/enable/disable/update）
// 的统一入参。cli 为开发工具维度（v1 仅接受 "claude"，其余返回「暂不支持」）——
// 预留多 CLI 扩展口子：新增 CLI 时在 service.Operate 的分发处增分支。
type PluginOperationRequest struct {
	CLI         string `json:"cli" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Marketplace string `json:"marketplace" binding:"required"`
}

// PluginMarketplaceListResponseData 是 getList 与全部写操作（add/remove/update/operate）的统一响应：
// 写操作成功后回读投影返回最新列表，前端一次往返即完成刷新。
type PluginMarketplaceListResponseData struct {
	Clis         []string                        `json:"clis"` // 本域支持安装操作的 CLI 清单（v1：["claude"]）
	Marketplaces []PluginMarketplaceResponseData `json:"marketplaces"`
}

// PluginMarketplaceResponseData 是一个插件市场的投影（市场注册表 × 清单扫描 × 安装状态）。
type PluginMarketplaceResponseData struct {
	Name            string                          `json:"name"`
	SourceType      string                          `json:"sourceType"`      // local | github | git | 其他 CLI 原始值
	SourceDetail    string                          `json:"sourceDetail"`    // 本地路径 / owner/repo / git URL
	InstallLocation string                          `json:"installLocation"` // claude 侧市场根目录（本地源=原路径；克隆源=clone 目录）
	Description     string                          `json:"description"`     // 市场清单 description（含 metadata 备选位置）
	Version         string                          `json:"version"`
	SupportedClis   []string                        `json:"supportedClis"` // 清单目录探测（v1：["claude"]）
	Plugins         []MarketplacePluginResponseData `json:"plugins"`
	ScanError       string                          `json:"scanError"` // 清单扫描失败原因；空 = 正常
}

// MarketplacePluginResponseData 是市场内一个插件条目的投影（清单信息 × claude 安装状态 join）。
type MarketplacePluginResponseData struct {
	Name             string                       `json:"name"`
	Source           string                       `json:"source"`         // 相对路径或紧凑 JSON（外部引用）
	SourceExternal   bool                         `json:"sourceExternal"` // 外部引用（github/npm 等）无法本地统计组件
	Dir              string                       `json:"dir"`            // 插件目录绝对路径；外部引用为空
	Description      string                       `json:"description"`
	Version          string                       `json:"version"`
	Components       PluginComponentsResponseData `json:"components"`
	Installed        bool                         `json:"installed"`
	Enabled          bool                         `json:"enabled"`
	Scope            string                       `json:"scope"`            // join 命中的安装 scope（user/project/local）；未安装为空
	InstalledVersion string                       `json:"installedVersion"` // claude 侧记录的已安装版本
}

// PluginComponentsResponseData 是插件组件构成统计（按官方标准布局扫描）。
type PluginComponentsResponseData struct {
	Commands   int `json:"commands"`
	Skills     int `json:"skills"`
	Agents     int `json:"agents"`
	Hooks      int `json:"hooks"`
	McpServers int `json:"mcpServers"`
}
