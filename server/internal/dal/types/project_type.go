package types

/*
 * 每 action 一个独立 Request 类型（不复用，便于各自演进与校验）：类型挂 project，项目内任务共享；
 * sort_order 由 service 自算（同项目 MAX+10000），故 Create 不含 sortOrder。
 */

// ProjectTypeGetListRequest 是 POST /api/project/type/getList 的入参（按 projectId 查全部）。
type ProjectTypeGetListRequest struct {
	ProjectID string `json:"projectId" binding:"required"`
}

// ProjectTypeGetInfoRequest 是 POST /api/project/type/getInfo 的入参。
type ProjectTypeGetInfoRequest struct {
	ID int `json:"id" binding:"required"`
}

// ProjectTypeCreateRequest 是 POST /api/project/type/create 的入参。
type ProjectTypeCreateRequest struct {
	ProjectID   string `json:"projectId" binding:"required"`
	Name        string `json:"name" binding:"required,max=100"`
	Color       string `json:"color" binding:"omitempty,max=20"`
	Description string `json:"description" binding:"omitempty,max=500"`
}

// ProjectTypeUpdateRequest 是 POST /api/project/type/update 的入参（不变更 projectId/sortOrder）。
type ProjectTypeUpdateRequest struct {
	ID          int    `json:"id" binding:"required"`
	Name        string `json:"name" binding:"required,max=100"`
	Color       string `json:"color" binding:"omitempty,max=20"`
	Description string `json:"description" binding:"omitempty,max=500"`
}

// ProjectTypeDeleteRequest 是 POST /api/project/type/delete 的入参。
type ProjectTypeDeleteRequest struct {
	ID int `json:"id" binding:"required"`
}
