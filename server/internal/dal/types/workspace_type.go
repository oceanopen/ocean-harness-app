package types

/*
 * 每 action 一个独立 Request 类型（不复用，便于各自演进与校验）：类型挂 project，项目内任务共享；
 * sort_order 由 service 自算（同项目 MAX+10000），故 Create 不含 sortOrder。
 */

// WorkspaceTypeGetListRequest 是 POST /api/tracker/workspaceType/getList 的入参（按 projectId 查全部）。
type WorkspaceTypeGetListRequest struct {
	ProjectID string `json:"projectId" binding:"required"`
}

// WorkspaceTypeGetInfoRequest 是 POST /api/tracker/workspaceType/getInfo 的入参。
type WorkspaceTypeGetInfoRequest struct {
	ID int `json:"id" binding:"required"`
}

// WorkspaceTypeCreateRequest 是 POST /api/tracker/workspaceType/create 的入参。
type WorkspaceTypeCreateRequest struct {
	ProjectID   string `json:"projectId" binding:"required"`
	Name        string `json:"name" binding:"required,max=100"`
	Color       string `json:"color" binding:"omitempty,max=20"`
	Description string `json:"description" binding:"omitempty,max=500"`
}

// WorkspaceTypeUpdateRequest 是 POST /api/tracker/workspaceType/update 的入参（不变更 projectId/sortOrder）。
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
