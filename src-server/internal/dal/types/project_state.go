package types

import (
	"we-claude-terminal/go-server/internal/dal/enums"
)

// 状态改为引用模型（docs/issue.md §3）：数据行只存 (state_group_code, state_code)，
// 展示元数据由目录 StateGroupCatalog/StateCatalog 提供。状态管理随项目 create/update
// 全量提交（统一走 ProjectStateItem 列表），不再有独立的 create/update/delete/reorder 接口。

// ProjectStateGetListRequest 是 POST /api/tracker/projectState/getList 的入参（按 projectId 查）。
type ProjectStateGetListRequest struct {
	ProjectID int `json:"projectId" binding:"required"`
}

// ProjectStateItem 是项目 create/update 入参里的一条状态（全量列表，引用目录）。
// service 层校验：(stateGroupCode, stateCode) 须命中 StateCatalog；每 group ≥1 项；is_default 恰好一个。
type ProjectStateItem struct {
	StateGroupCode enums.StateGroup `json:"stateGroupCode"`
	StateCode      string           `json:"stateCode"`
	SortOrder      float64          `json:"sortOrder"`
	IsDefault      enums.YesNo      `json:"isDefault"`
}

// CatalogResponse 是 GET /api/tracker/projectState/catalog 的响应：固定状态目录（与项目无关）。
// groups = 分组展示元数据（StateGroupCatalog）；states = 全部子状态定义（StateCatalog，扁平，前端按 groupCode 自行分组）。
type CatalogResponse struct {
	Groups []enums.StateGroupMeta `json:"groups"`
	States []enums.StateMeta       `json:"states"`
}
