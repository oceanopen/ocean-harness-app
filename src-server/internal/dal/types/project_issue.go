package types

import (
	"we-claude-terminal/go-server/internal/dal/enums"
	"we-claude-terminal/go-server/internal/dal/model"
)

// 每 action 一个独立 Request 类型。issue 用全局自增 id 标识（无 issue key）；
// stateId 由 create 时取 project.default_state_id（不入参）；sortOrder 由 service 自算（不入参）；
// priority/isDraft 为 typed 枚举（前端传，空值由 service 规范为 none/N）。

// ProjectIssueGetListRequest 是 POST /api/tracker/projectIssue/getList 的入参。
// groupBy 由前端对扁平列表自行分组，后端不接收；orderBy 不传则按 sort_order 升序。
type ProjectIssueGetListRequest struct {
	ProjectID int            `json:"projectId" binding:"required"`
	OrderBy   string         `json:"orderBy"` // id/sort_order/priority/created_at，空则 sort_order
	StateID   int            `json:"stateId"`
	Priority  enums.Priority `json:"priority"`
	LabelID   int            `json:"labelId"`
	Keyword   string         `json:"keyword"`
}

// ProjectIssueGetInfoRequest 是 POST /api/tracker/projectIssue/getInfo 的入参。
type ProjectIssueGetInfoRequest struct {
	ID int `json:"id" binding:"required"`
}

// ProjectIssueCreateRequest 是 POST /api/tracker/projectIssue/create 的入参。
// stateId 不传（后端取 project.default_state_id）；sortOrder 不传（后端自算同 project MAX+10000）。
type ProjectIssueCreateRequest struct {
	ProjectID   int            `json:"projectId" binding:"required"`
	WorkspaceID int            `json:"workspaceId" binding:"required"`
	Name        string         `json:"name" binding:"required,max=255"`
	Description string         `json:"description" binding:"omitempty"`
	Priority    enums.Priority `json:"priority"`
	IsDraft     enums.YesNo    `json:"isDraft"`
	StartDate   string         `json:"startDate" binding:"omitempty"`
	TargetDate  string         `json:"targetDate" binding:"omitempty"`
}

// ProjectIssueUpdateRequest 是 POST /api/tracker/projectIssue/update 的入参。
// stateId 变化触发 completed_at 流转：新 state 的 state_group=completed→写 now，否则清 NULL。
// 不变更 projectId/workspaceId/sortOrder（sortOrder 后续拖拽迭代维护）。
type ProjectIssueUpdateRequest struct {
	ID          int            `json:"id" binding:"required"`
	Name        string         `json:"name" binding:"required,max=255"`
	Description string         `json:"description" binding:"omitempty"`
	StateID     int            `json:"stateId"`
	Priority    enums.Priority `json:"priority"`
	IsDraft     enums.YesNo    `json:"isDraft"`
	StartDate   string         `json:"startDate" binding:"omitempty"`
	TargetDate  string         `json:"targetDate" binding:"omitempty"`
}

// ProjectIssueMoveRequest 是 POST /api/tracker/projectIssue/move 的入参（看板拖拽单卡移动）。
// 前端按分数插值算好 sortOrder 传上来后端写库；stateId 变化触发 completed_at 流转。
// 不碰 name/description/priority 等业务字段（由 update 维护）。
type ProjectIssueMoveRequest struct {
	ID        int     `json:"id" binding:"required"`
	StateID   int     `json:"stateId" binding:"required"`
	SortOrder float64 `json:"sortOrder"`
}

// ProjectIssueDeleteRequest 是 POST /api/tracker/projectIssue/delete 的入参。
type ProjectIssueDeleteRequest struct {
	ID int `json:"id" binding:"required"`
}

// ProjectIssueResponseData 是 issue 的响应：嵌入 DO（JSON 平铺 issue 字段）+ 应用层组装的 label 列表。
// completedAt 为 *time.Time：未完成=null / 完成=时间。
type ProjectIssueResponseData struct {
	*model.ProjectIssue
	Labels []*model.WorkspaceLabel `json:"labels"`
}
