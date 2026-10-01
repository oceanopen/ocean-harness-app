package types

// agent catalog 域 DTO：无本地表，SSOT 为 sidecar 内嵌快照（internal/agentcatalog）。
// 手写镜像前端 AgentCatalogService 的 TS interface（项目约定，无生成器）；
// spawn 细节（command/args/env）属后端执行面，不入前端契约。

// AgentCatalogGetListRequest 是 POST /api/agentCatalog/getList 的入参（无筛选）。
type AgentCatalogGetListRequest struct{}

// AgentCatalogEntryResponseData 是 catalog 条目的前端投影。
type AgentCatalogEntryResponseData struct {
	ID             string `json:"id"`
	Code           string `json:"code"` // agentCode
	Label          string `json:"label"`
	Version        string `json:"version"`
	Description    string `json:"description"`
	Strategy       string `json:"strategy"`
	Enabled        bool   `json:"enabled"`
	NodeMinVersion string `json:"nodeMinVersion"` // node 主版本下限（T1.4 doctor 消费；空 = 不校验）
}
