package types

import (
	"we-claude-terminal/go-server/internal/dal/enums"
)

// 每 action 一个独立 Request 类型（不复用，便于各自演进与校验）。常规字段用 gin binding tag 校验；
// stateGroup 为 typed 枚举 enums.StateGroup（非法值由 Value() 在写库时兜底校验），空值由 service 规范为 backlog。

// ProjectStateGetListRequest 是 POST /api/tracker/projectState/getList 的入参（按 projectId 查）。
type ProjectStateGetListRequest struct {
	ProjectID int `json:"projectId" binding:"required"`
}

// ProjectStateCreateRequest 是 POST /api/tracker/projectState/create 的入参。
// sort_order 由前端手动传入（migration 默认 0）；workspaceId 由前端导航上下文提供（无 FK 设计）。
type ProjectStateCreateRequest struct {
	ProjectID   int              `json:"projectId" binding:"required"`
	WorkspaceID int              `json:"workspaceId" binding:"required"`
	Name        string           `json:"name" binding:"required,max=100"`
	Color       string           `json:"color" binding:"omitempty,max=20"`
	Slug        string           `json:"slug" binding:"omitempty,max=50"`
	SortOrder   float64          `json:"sortOrder"`
	StateGroup  enums.StateGroup `json:"stateGroup"`
	IsDefault   enums.YesNo      `json:"isDefault"`
	IsTriage    enums.YesNo      `json:"isTriage"`
}

// ProjectStateUpdateRequest 是 POST /api/tracker/projectState/update 的入参。
// 不变更 projectId / workspaceId / sortOrder（sortOrder 由 reorder 专接口维护）。
type ProjectStateUpdateRequest struct {
	ID         int              `json:"id" binding:"required"`
	Name       string           `json:"name" binding:"required,max=100"`
	Color      string           `json:"color" binding:"omitempty,max=20"`
	Slug       string           `json:"slug" binding:"omitempty,max=50"`
	StateGroup enums.StateGroup `json:"stateGroup"`
	IsDefault  enums.YesNo      `json:"isDefault"`
	IsTriage   enums.YesNo      `json:"isTriage"`
}

// ProjectStateDeleteRequest 是 POST /api/tracker/projectState/delete 的入参。
type ProjectStateDeleteRequest struct {
	ID int `json:"id" binding:"required"`
}

// ProjectStateReorderItem 是 reorder 的单条排序项（显式指定新 sort_order）。
type ProjectStateReorderItem struct {
	ID        int     `json:"id" binding:"required"`
	SortOrder float64 `json:"sortOrder"`
}

// ProjectStateReorderRequest 是 POST /api/tracker/projectState/reorder 的入参（按 projectId 批量调 sort_order）。
type ProjectStateReorderRequest struct {
	ProjectID int                       `json:"projectId" binding:"required"`
	Items     []ProjectStateReorderItem `json:"items" binding:"required,min=1"`
}
