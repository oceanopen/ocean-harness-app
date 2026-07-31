package types

// 每 action 一个独立 Request 类型（不复用，便于各自演进与校验）。
// workspaceLabel 只挂 workspace（所有项目共享一套通用标签），无 project 级归属；
// sort_order 由 service 自算（同 workspace MAX+10000），故 Create 不含 sortOrder。

// WorkspaceLabelGetListRequest 是 POST /api/tracker/workspaceLabel/getList 的入参（按 workspaceId 查全部）。
type WorkspaceLabelGetListRequest struct {
	WorkspaceID int `json:"workspaceId" binding:"required"`
}

// WorkspaceLabelGetInfoRequest 是 POST /api/tracker/workspaceLabel/getInfo 的入参。
type WorkspaceLabelGetInfoRequest struct {
	ID int `json:"id" binding:"required"`
}

// WorkspaceLabelCreateRequest 是 POST /api/tracker/workspaceLabel/create 的入参。
type WorkspaceLabelCreateRequest struct {
	WorkspaceID int    `json:"workspaceId" binding:"required"`
	Name        string `json:"name" binding:"required,max=100"`
	Color       string `json:"color" binding:"omitempty,max=20"`
	Description string `json:"description" binding:"omitempty,max=500"`
}

// WorkspaceLabelUpdateRequest 是 POST /api/tracker/workspaceLabel/update 的入参。
// 不变更 workspaceId / sortOrder（sortOrder 后续按需加 reorder 接口维护）。
type WorkspaceLabelUpdateRequest struct {
	ID          int    `json:"id" binding:"required"`
	Name        string `json:"name" binding:"required,max=100"`
	Color       string `json:"color" binding:"omitempty,max=20"`
	Description string `json:"description" binding:"omitempty,max=500"`
}

// WorkspaceLabelDeleteRequest 是 POST /api/tracker/workspaceLabel/delete 的入参。
type WorkspaceLabelDeleteRequest struct {
	ID int `json:"id" binding:"required"`
}
