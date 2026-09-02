package types

import (
	"ocean-harness/src-server/internal/dal/enums"
	"ocean-harness/src-server/internal/dal/model"
)

// 每 action 一个独立 Request 类型。issue 主键 id 为 uuid 字符串（与 claude session_id 同格式，
// Create 时由 service 生成 uuid v7）；stateCode 为固定 5 值枚举（BACKLOG/TODO/IN_PROGRESS/DONE/CANCELLED，新建默认 BACKLOG）；
// sortOrder 由 service 自算（不入参）；priority/isDraft 为 typed 枚举（前端传，空值由 service 规范为 none/N）。

// ProjectIssueGetListRequest 是 POST /api/tracker/projectIssue/getList 的入参。
// 分组由前端对扁平列表自行分组，后端不接收；orderBy 不传则按 sort_order 升序（id 无数值序，按 id 排序请传 created_at）。
type ProjectIssueGetListRequest struct {
	ProjectID int             `json:"projectId" binding:"required"`
	OrderBy   string          `json:"orderBy"` // created_at/sort_order/priority，空则 sort_order
	StateCode enums.StateCode `json:"stateCode"`
	Priority  enums.Priority  `json:"priority"`
	LabelID   int             `json:"labelId"`
	Keyword   string          `json:"keyword"`
}

// ProjectIssueGetInfoRequest 是 POST /api/tracker/projectIssue/getInfo 的入参。
type ProjectIssueGetInfoRequest struct {
	ID string `json:"id" binding:"required"`
}

// IssueRepositoryBranch 是 issue 关联的单个「仓库+分支」项（多选）。字段对齐前端 IssueRepositoryBranchModel。
type IssueRepositoryBranch struct {
	LocalRepositoryID int    `json:"localRepositoryId"` // 须属于 issue 所属项目的关联仓库
	RepositoryBranch  string `json:"repositoryBranch"`  // 分支名（freeSolo 可手输，不校验存在性）
}

// ProjectIssueCreateRequest 是 POST /api/tracker/projectIssue/create 的入参。
// stateCode 空值取默认 BACKLOG；sortOrder 不传（后端自算同 project MAX+10000）。
// parentId 非空时创建为子任务（后端校验父存在 + 同 project + 仅一层）；id 由后端生成 uuid。
type ProjectIssueCreateRequest struct {
	ProjectID            int                     `json:"projectId" binding:"required"`
	WorkspaceID          int                     `json:"workspaceId" binding:"required"`
	Name                 string                  `json:"name" binding:"required,max=255"`
	Description          string                  `json:"description" binding:"omitempty"`
	Priority             enums.Priority          `json:"priority"`
	IsDraft              enums.YesNo             `json:"isDraft"`
	StartDate            string                  `json:"startDate" binding:"omitempty"`
	TargetDate           string                  `json:"targetDate" binding:"omitempty"`
	StateCode            enums.StateCode         `json:"stateCode"`            // 空值 → 默认 BACKLOG
	ParentID             string                  `json:"parentId"`             // ""=顶级，非空=子任务（须与父同 project，仅一层）
	LabelIDs             []int                   `json:"labelIds"`             // 全量覆盖该 issue 的 label 关联
	RepositoryBranchList []IssueRepositoryBranch `json:"repositoryBranchList"` // 全量覆盖关联的仓库+分支列表（逐项校验仓库归属）
}

// ProjectIssueUpdateRequest 是 POST /api/tracker/projectIssue/update 的入参。
// stateCode 变化触发 completed_at 流转：DONE→写 now，否则清 NULL。
// labelIds 全量覆盖该 issue 的 label 关联（事务内 diff：删多余/插入新增）。
// 不变更 projectId/workspaceId/sortOrder（sortOrder 后续拖拽迭代维护）。
type ProjectIssueUpdateRequest struct {
	ID                   string                  `json:"id" binding:"required"`
	Name                 string                  `json:"name" binding:"required,max=255"`
	Description          string                  `json:"description" binding:"omitempty"`
	StateCode            enums.StateCode         `json:"stateCode"`
	Priority             enums.Priority          `json:"priority"`
	IsDraft              enums.YesNo             `json:"isDraft"`
	StartDate            string                  `json:"startDate" binding:"omitempty"`
	TargetDate           string                  `json:"targetDate" binding:"omitempty"`
	LabelIDs             []int                   `json:"labelIds"`             // 全量覆盖该 issue 的 label 关联
	RepositoryBranchList []IssueRepositoryBranch `json:"repositoryBranchList"` // 全量覆盖关联的仓库+分支列表（逐项校验仓库归属）
}

// ProjectIssueMoveRequest 是 POST /api/tracker/projectIssue/move 的入参（看板拖拽单卡移动）。
// 前端按分数插值算好 sortOrder 传上来后端写库；stateCode 变化触发 completed_at 流转。
// 不碰 name/description/priority 等业务字段（由 update 维护）。
type ProjectIssueMoveRequest struct {
	ID        string          `json:"id" binding:"required"`
	StateCode enums.StateCode `json:"stateCode" binding:"required"`
	SortOrder float64         `json:"sortOrder"`
}

// ProjectIssueDeleteRequest 是 POST /api/tracker/projectIssue/delete 的入参。
type ProjectIssueDeleteRequest struct {
	ID string `json:"id" binding:"required"`
}

// ProjectIssueResponseData 是 issue 的响应：嵌入 DO（JSON 平铺 issue 字段）+ 应用层组装的
// label 列表与关联仓库+分支列表（来自 t_issue_local_repositories 批量组装，见 assembleWithLabels）。
// completedAt 为 *time.Time：未完成=null / 完成=时间。
// 注：DO 的 IssueLocalRepositoryList 从不 Preload（手动批量组装），omitempty 恒不输出。
type ProjectIssueResponseData struct {
	*model.ProjectIssue
	Labels               []*model.WorkspaceLabel `json:"labels"`
	RepositoryBranchList []IssueRepositoryBranch `json:"repositoryBranchList"`
}
