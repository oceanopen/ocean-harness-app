package types

// 每 action 一个独立 Request 类型（不复用，便于各自演进与校验）。常规字段用 gin binding tag 校验；
// 跨字段/复杂场景可追加 vd tag（go-tagexpr），由 apis.Api.Validate 自动生效。

// WorkspaceGetListRequest 是 POST /api/tracker/workspace/getList 的入参（当前无参，预留筛选位）。
type WorkspaceGetListRequest struct{}

// WorkspaceGetInfoRequest 是 POST /api/tracker/workspace/getInfo 的入参。
type WorkspaceGetInfoRequest struct {
	ID int `json:"id" binding:"required"`
}

// WorkspaceCreateRequest 是 POST /api/tracker/workspace/create 的入参。
type WorkspaceCreateRequest struct {
	Name        string `json:"name" binding:"required,max=100"`
	Slug        string `json:"slug" binding:"required,max=100"`
	Description string `json:"description" binding:"omitempty,max=500"`
}

// WorkspaceUpdateRequest 是 POST /api/tracker/workspace/update 的入参。
type WorkspaceUpdateRequest struct {
	ID          int    `json:"id" binding:"required"`
	Name        string `json:"name" binding:"required,max=100"`
	Slug        string `json:"slug" binding:"required,max=100"`
	Description string `json:"description" binding:"omitempty,max=500"`
}

// WorkspaceDeleteRequest 是 POST /api/tracker/workspace/delete 的入参。
type WorkspaceDeleteRequest struct {
	ID int `json:"id" binding:"required"`
}
