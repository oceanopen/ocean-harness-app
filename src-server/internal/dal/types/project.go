package types

import (
	"we-claude-terminal/go-server/internal/dal/model"
)

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
// 不变更 workspaceId。LocalRepositoryIDs 为关联仓库最终列表（全量覆盖：先删后插，无 diff）。
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

// 项目 ↔ 本地仓库 多对多关联：随 create/update 全量保存（无独立增删接口）；
// 关联仓库 ids 随项目响应返回（ProjectResponseData.LocalRepositoryIDs），无需独立读接口。

// ProjectResponseData 是项目响应：嵌入 DO（JSON 平铺项目字段）+ 应用层装配的关联仓库 id 列表。
// LocalRepositoryIDs 由 getList/getInfo Preload(ProjectLocalRepositoryList) 后提取，转换后清空原列表去冗余。
type ProjectResponseData struct {
	*model.WorkspaceProject
	LocalRepositoryIDs []int `json:"localRepositoryIds"`
}
