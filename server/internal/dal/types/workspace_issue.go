package types

import (
	"ocean-harness/server/internal/dal/model"
)

/*
 * 每 action 一个独立 Request 类型：任务主键 id 为 uuid 字符串（与 claude session_id 同格式，Create 时由 service 生成 uuid v7）；
 * 排序恒按 created_at 升序（无 sort_order 列，看板拖拽随五状态一并移除）。
 */

// WorkspaceIssueGetListRequest 是 POST /api/tracker/workspaceIssue/getList 的入参：分组由前端对扁平列表自行分组，后端不接收。
type WorkspaceIssueGetListRequest struct {
	ProjectID string `json:"projectId" binding:"required"`
	TypeID    int    `json:"typeId"` // >0 按类型筛选（任务.type_id 单值等值匹配）
	Keyword   string `json:"keyword"`
}

// WorkspaceIssueGetInfoRequest 是 POST /api/tracker/workspaceIssue/getInfo 的入参。
type WorkspaceIssueGetInfoRequest struct {
	ID string `json:"id" binding:"required"`
}

// IssueRepositoryBranch 是任务关联的单个「仓库+分支」项（多选）。字段对齐前端 IssueRepositoryBranchModel。
type IssueRepositoryBranch struct {
	LocalRepositoryID int    `json:"localRepositoryId"`
	RepositoryBranch  string `json:"repositoryBranch"` // 分支名（freeSolo 可手输，不校验存在性）
}

/*
 * WorkspaceIssueCreateRequest 是 POST /api/tracker/workspaceIssue/create 的入参：id 由后端生成 uuid；
 * typeId 必选默认由前端保证（0=未分类）。
 */
type WorkspaceIssueCreateRequest struct {
	ProjectID            string                   `json:"projectId" binding:"required"`
	Name                 string                   `json:"name" binding:"required,max=255"`
	Description          string                   `json:"description" binding:"omitempty"`
	TypeID               int                      `json:"typeId"`                             // 任务类型（t_project_types 单值引用，0=未分类）
	RepositoryBranchList []IssueRepositoryBranch  `json:"repositoryBranchList"`               // 全量覆盖关联的仓库+分支列表
	LaunchSettings       *WorkspaceLaunchSettings `json:"launchSettings" binding:"omitempty"` // 启动设置覆盖（nil = 未覆盖，消费端回落项目）
}

/*
 * WorkspaceIssueUpdateRequest 是 POST /api/tracker/workspaceIssue/update 的入参：typeId 为 *int：
 * nil=保留原值（MCP 部分更新回填现值），非 nil 覆写（0=未分类）；不变更 projectId。
 */
type WorkspaceIssueUpdateRequest struct {
	ID                   string                   `json:"id" binding:"required"`
	Name                 string                   `json:"name" binding:"required,max=255"`
	Description          string                   `json:"description" binding:"omitempty"`
	TypeID               *int                     `json:"typeId"`                             // nil=保留原值，0=置为未分类
	RepositoryBranchList []IssueRepositoryBranch  `json:"repositoryBranchList"`               // 全量覆盖关联的仓库+分支列表
	LaunchSettings       *WorkspaceLaunchSettings `json:"launchSettings" binding:"omitempty"` // 启动设置覆盖（nil = 清除覆盖，消费端回落项目）
}

// WorkspaceIssueDeleteRequest 是 POST /api/tracker/workspaceIssue/delete 的入参。
type WorkspaceIssueDeleteRequest struct {
	ID string `json:"id" binding:"required"`
}

/*
 * WorkspaceIssueResponseData 是任务的响应：嵌入 DO（JSON 平铺任务字段）+ 应用层组装的类型与关联仓库+分支列表（来自 t_task_local_repositories 批量组装，见 assembleWithType）；
 * type 为单值（type_id=0 时为 null）；注：DO 的 TaskLocalRepositoryList 从不 Preload（手动批量组装），omitempty 恒不输出。
 */
type WorkspaceIssueResponseData struct {
	*model.ProjectTask
	Type                 *model.ProjectType      `json:"type"`
	RepositoryBranchList []IssueRepositoryBranch `json:"repositoryBranchList"`
	/*
	 * 启动设置覆盖：浅层同名字段遮蔽嵌入 DO 的 launch_settings string（Go json 浅深度胜出），输出解析后的结构（nil = 未覆盖，消费端回落项目）。
	 */
	LaunchSettings *WorkspaceLaunchSettings `json:"launchSettings"`
}
