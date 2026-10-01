package service

import (
	"ocean-harness/server/internal/agentcatalog"
	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
)

// AgentCatalog 对应 /api/agentCatalog 命名空间下的业务逻辑。
// 无本地表：SSOT 为 sidecar 内嵌离线快照（internal/agentcatalog，编译期 go:embed），
// 本 service 只做前端投影（值域 SSOT：launch_settings.agentCode 合法取值 = enabled 条目 id）。
type AgentCatalog struct {
	apis.Service
}

// GetList 返回全部 catalog 条目（含 enabled 标记，前端按 enabled 过滤可选面）。
func (svc AgentCatalog) GetList() ([]types.AgentCatalogEntryResponseData, error) {
	cat, err := agentcatalog.Load()
	if err != nil {
		return nil, err
	}
	entries := make([]types.AgentCatalogEntryResponseData, 0, len(cat.Agents))
	for _, e := range cat.Agents {
		entries = append(entries, types.AgentCatalogEntryResponseData{
			ID:             e.ID,
			Code:           e.Code,
			Label:          e.Label,
			Version:        e.Version,
			Description:    e.Description,
			Strategy:       string(e.Strategy),
			Enabled:        e.Enabled,
			NodeMinVersion: e.NodeMinVersion,
		})
	}
	return entries, nil
}
