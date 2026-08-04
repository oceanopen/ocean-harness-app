package types

// 每 action 一个独立 Request 类型（不复用，便于各自演进与校验）。常规字段用 gin binding tag 校验。
// 项目无短码（identifier）、允许重名（无业务唯一键），issue 用全局自增 id 标识；workspaceId 由前端导航上下文提供（无 FK 设计）。

// ProjectGetListRequest 是 POST /api/tracker/project/getList 的入参（按 workspaceId 查）。
type ProjectGetListRequest struct {
	WorkspaceID int `json:"workspaceId" binding:"required"`
}

// ProjectGetInfoRequest 是 POST /api/tracker/project/getInfo 的入参。
type ProjectGetInfoRequest struct {
	ID int `json:"id" binding:"required"`
}

// ProjectCreateRequest 是 POST /api/tracker/project/create 的入参。
// LocalRepositoryIDs 为关联仓库最终列表（全量，随项目一起保存，事务内全量写入）。
type ProjectCreateRequest struct {
	WorkspaceID        int    `json:"workspaceId" binding:"required"`
	Name               string `json:"name" binding:"required,max=100"`
	Description        string `json:"description" binding:"omitempty,max=500"`
	Emoji              string `json:"emoji" binding:"omitempty,max=20"`
	LocalRepositoryIDs []int  `json:"localRepositoryIds"`
}

// ProjectUpdateRequest 是 POST /api/tracker/project/update 的入参。
// 不变更 workspaceId / defaultStateId（defaultStateId 由 state 模块维护）。
// LocalRepositoryIDs 为关联仓库最终列表（全量覆盖：先删后插，无 diff）。
type ProjectUpdateRequest struct {
	ID                 int    `json:"id" binding:"required"`
	Name               string `json:"name" binding:"required,max=100"`
	Description        string `json:"description" binding:"omitempty,max=500"`
	Emoji              string `json:"emoji" binding:"omitempty,max=20"`
	LocalRepositoryIDs []int  `json:"localRepositoryIds"`
}

// ProjectDeleteRequest 是 POST /api/tracker/project/delete 的入参。
type ProjectDeleteRequest struct {
	ID int `json:"id" binding:"required"`
}

// 项目 ↔ 本地仓库 多对多关联：随 create/update 全量保存，无独立增删接口。
// listRepositories 为读接口（编辑回显 + issue 仓库下拉）。

// ProjectListRepositoriesRequest 是 POST /api/tracker/project/listRepositories 的入参（列项目已关联的仓库）。
type ProjectListRepositoriesRequest struct {
	ProjectID int `json:"projectId" binding:"required"`
}
