package types

import (
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
)

/*
 * 每 action 一个独立 Request 类型：issue 主键 id 为 uuid 字符串（与 claude session_id 同格式，Create 时由 service 生成 uuid v7）；
 * stateCode 为固定 5 值枚举（BACKLOG/TODO/IN_PROGRESS/DONE/CANCELLED，新建默认 BACKLOG）；
 * sortOrder 由 service 自算（不入参）。
 */

/*
 * WorkspaceIssueGetListRequest 是 POST /api/tracker/workspaceIssue/getList 的入参：分组由前端对扁平列表自行分组，后端不接收；
 * orderBy 不传则按 sort_order 升序。
 */
type WorkspaceIssueGetListRequest struct {
	WorkspaceID string          `json:"workspaceId" binding:"required"`
	OrderBy     string          `json:"orderBy"` // created_at/sort_order，空则 sort_order
	StateCode   enums.StateCode `json:"stateCode"`
	TypeID      int             `json:"typeId"` // >0 按类型筛选（issue.type_id 单值等值匹配）
	Keyword     string          `json:"keyword"`
}

// WorkspaceIssueGetInfoRequest 是 POST /api/tracker/workspaceIssue/getInfo 的入参。
type WorkspaceIssueGetInfoRequest struct {
	ID string `json:"id" binding:"required"`
}

// IssueRepositoryBranch 是 issue 关联的单个「仓库+分支」项（多选）。字段对齐前端 IssueRepositoryBranchModel。
type IssueRepositoryBranch struct {
	LocalRepositoryID int    `json:"localRepositoryId"` // 须属于 issue 所属工作空间的关联仓库
	RepositoryBranch  string `json:"repositoryBranch"`  // 分支名（freeSolo 可手输，不校验存在性）
}

/*
 * WorkspaceIssueCreateRequest 是 POST /api/tracker/workspaceIssue/create 的入参：stateCode 空值取默认 BACKLOG，sortOrder 不传（后端自算同 workspace MAX+10000）；
 * id 由后端生成 uuid。
 */
type WorkspaceIssueCreateRequest struct {
	WorkspaceID          string                   `json:"workspaceId" binding:"required"`
	Name                 string                   `json:"name" binding:"required,max=255"`
	Description          string                   `json:"description" binding:"omitempty"`
	StartDate            string                   `json:"startDate" binding:"omitempty"`
	TargetDate           string                   `json:"targetDate" binding:"omitempty"`
	StateCode            enums.StateCode          `json:"stateCode"`                          // 空值 → 默认 BACKLOG
	TypeID               int                      `json:"typeId"`                             // issue 类型（t_workspace_types 单值引用，0=未分类；必选默认由前端保证）
	RepositoryBranchList []IssueRepositoryBranch  `json:"repositoryBranchList"`               // 全量覆盖关联的仓库+分支列表（逐项校验仓库归属）
	LaunchSettings       *WorkspaceLaunchSettings `json:"launchSettings" binding:"omitempty"` // 启动设置覆盖（nil = 未覆盖，消费端回落 workspace）
}

/*
 * WorkspaceIssueUpdateRequest 是 POST /api/tracker/workspaceIssue/update 的入参：stateCode 变化触发 completed_at 流转（DONE→写 now，否则清 NULL）；
 * typeId 为 *int：nil=保留原值（MCP 部分更新回填现值），非 nil 覆写（0=未分类）；
 * 不变更 workspaceId/sortOrder（sortOrder 后续拖拽迭代维护）。
 */
type WorkspaceIssueUpdateRequest struct {
	ID                   string                   `json:"id" binding:"required"`
	Name                 string                   `json:"name" binding:"required,max=255"`
	Description          string                   `json:"description" binding:"omitempty"`
	StateCode            enums.StateCode          `json:"stateCode"`
	StartDate            string                   `json:"startDate" binding:"omitempty"`
	TargetDate           string                   `json:"targetDate" binding:"omitempty"`
	TypeID               *int                     `json:"typeId"`                             // nil=保留原值，0=置为未分类
	RepositoryBranchList []IssueRepositoryBranch  `json:"repositoryBranchList"`               // 全量覆盖关联的仓库+分支列表（逐项校验仓库归属）
	LaunchSettings       *WorkspaceLaunchSettings `json:"launchSettings" binding:"omitempty"` // 启动设置覆盖（nil = 清除覆盖，消费端回落 workspace）
}

/*
 * WorkspaceIssueMoveRequest 是 POST /api/tracker/workspaceIssue/move 的入参（看板拖拽单卡移动）：前端按分数插值算好 sortOrder 传上来后端写库，stateCode 变化触发 completed_at 流转；
 * 不碰 name/description 等业务字段（由 update 维护）。
 */
type WorkspaceIssueMoveRequest struct {
	ID        string          `json:"id" binding:"required"`
	StateCode enums.StateCode `json:"stateCode" binding:"required"`
	SortOrder float64         `json:"sortOrder"`
}

// WorkspaceIssueDeleteRequest 是 POST /api/tracker/workspaceIssue/delete 的入参。
type WorkspaceIssueDeleteRequest struct {
	ID string `json:"id" binding:"required"`
}

/*
 * WorkspaceIssueResponseData 是 issue 的响应：嵌入 DO（JSON 平铺 issue 字段）+ 应用层组装的类型与关联仓库+分支列表（来自 t_issue_local_repositories 批量组装，见 assembleWithType）；
 * completedAt 为 *time.Time：未完成=null / 完成=时间，type 为单值（type_id=0 时为 null）；
 * 注：DO 的 IssueLocalRepositoryList 从不 Preload（手动批量组装），omitempty 恒不输出。
 */
type WorkspaceIssueResponseData struct {
	*model.WorkspaceIssue
	Type                 *model.WorkspaceType    `json:"type"`
	RepositoryBranchList []IssueRepositoryBranch `json:"repositoryBranchList"`
	/*
	 * 启动设置覆盖：浅层同名字段遮蔽嵌入 DO 的 launch_settings string（Go json 浅深度胜出），输出解析后的结构（nil = 未覆盖，消费端回落 workspace）。
	 */
	LaunchSettings *WorkspaceLaunchSettings `json:"launchSettings"`
}
