package types

// 每 action 一个独立 Request 类型（不复用，便于各自演进与校验）。
// workspaceType 只挂 workspace（所有项目共享一套通用类型），无 project 级归属；
// sort_order 由 service 自算（同 workspace MAX+10000），故 Create 不含 sortOrder。
// 原「标签」（workspaceLabel）域，2026-09-24 更名为「类型」。

// WorkspaceTypeGetListRequest 是 POST /api/tracker/workspaceType/getList 的入参（按 workspaceId 查全部）。
type WorkspaceTypeGetListRequest struct {
	WorkspaceID int `json:"workspaceId" binding:"required"`
}

// WorkspaceTypeGetInfoRequest 是 POST /api/tracker/workspaceType/getInfo 的入参。
type WorkspaceTypeGetInfoRequest struct {
	ID int `json:"id" binding:"required"`
}

// WorkspaceTypeCreateRequest 是 POST /api/tracker/workspaceType/create 的入参。
type WorkspaceTypeCreateRequest struct {
	WorkspaceID int    `json:"workspaceId" binding:"required"`
	Name        string `json:"name" binding:"required,max=100"`
	Color       string `json:"color" binding:"omitempty,max=20"`
	Description string `json:"description" binding:"omitempty,max=500"`
}

// WorkspaceTypeUpdateRequest 是 POST /api/tracker/workspaceType/update 的入参。
// 不变更 workspaceId / sortOrder（sortOrder 后续按需加 reorder 接口维护）。
type WorkspaceTypeUpdateRequest struct {
	ID          int    `json:"id" binding:"required"`
	Name        string `json:"name" binding:"required,max=100"`
	Color       string `json:"color" binding:"omitempty,max=20"`
	Description string `json:"description" binding:"omitempty,max=500"`
}

// WorkspaceTypeDeleteRequest 是 POST /api/tracker/workspaceType/delete 的入参。
type WorkspaceTypeDeleteRequest struct {
	ID int `json:"id" binding:"required"`
}
