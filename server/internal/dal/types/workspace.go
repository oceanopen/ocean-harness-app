package types

import (
	"encoding/json"
	"time"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
)

// 每 action 一个独立 Request 类型（不复用，便于各自演进与校验）。常规字段用 gin binding tag 校验；
// 跨字段/复杂场景可追加 vd tag（go-tagexpr），由 apis.Api.Validate 自动生效。

// WorkspaceGetListRequest 是 POST /api/tracker/workspace/getList 的入参（当前无参，预留筛选位）。
type WorkspaceGetListRequest struct{}

// WorkspaceGetInfoRequest 是 POST /api/tracker/workspace/getInfo 的入参。
type WorkspaceGetInfoRequest struct {
	ID int `json:"id" binding:"required"`
}

// WorkspaceLaunchSettings 启动设置（t_workspaces.launch_settings JSON 的请求/响应形态，
// shape SSOT）。mode/agentCode/permissionMode 的取值域由前端枚举约定（一期 UI 见方案文档
// T0.2/T0.3），后端透传不校验；空对象/空串 = 未配置，消费端等同 none（不做任何启动动作）。
type WorkspaceLaunchSettings struct {
	Mode           string          `json:"mode,omitempty"`           // none | terminal-manual | terminal-auto | acp（未配置等同 none：出启动方式选择面板；manual=自动打开终端）
	AgentCode      enums.AgentCode `json:"agentCode,omitempty"`      // ACP 会话 agent（catalog 条目）
	AutoCommand    string          `json:"autoCommand,omitempty"`    // 终端启动 Agent（仅 terminal-auto：主终端直接启动；manual 档经终端工具条自选；一期 claude）
	PermissionMode string          `json:"permissionMode,omitempty"` // ACP 执行模式：acceptEdits | bypassPermissions
}

// WorkspaceCreateRequest 是 POST /api/tracker/workspace/create 的入参。
// dir 为工作区目录（须为绝对路径，service 层校验可创建可写）。
// launchSettings 指针可空 = 不配置（消费端等同 none）。
type WorkspaceCreateRequest struct {
	Name           string                   `json:"name" binding:"required,max=100"`
	Dir            string                   `json:"dir" binding:"required"`
	Description    string                   `json:"description" binding:"omitempty,max=500"`
	LaunchSettings *WorkspaceLaunchSettings `json:"launchSettings" binding:"omitempty"`
}

// WorkspaceUpdateRequest 是 POST /api/tracker/workspace/update 的入参。
// launchSettings 为 nil = 清空配置（等同 none）；空对象同视为未配置。
type WorkspaceUpdateRequest struct {
	ID             int                      `json:"id" binding:"required"`
	Name           string                   `json:"name" binding:"required,max=100"`
	Dir            string                   `json:"dir" binding:"required"`
	Description    string                   `json:"description" binding:"omitempty,max=500"`
	LaunchSettings *WorkspaceLaunchSettings `json:"launchSettings" binding:"omitempty"`
}

// WorkspaceDeleteRequest 是 POST /api/tracker/workspace/delete 的入参。
type WorkspaceDeleteRequest struct {
	ID int `json:"id" binding:"required"`
}

// WorkspaceResponseData workspace 响应：launch_settings JSON 列反序列化为结构（空串/损坏
// → nil，未配置语义），扁平呈现形态由 FromModel 装配（不嵌入 DO model，对齐 im_bot 域）。
type WorkspaceResponseData struct {
	ID             int                      `json:"id"`
	Name           string                   `json:"name"`
	Dir            string                   `json:"dir"`
	Description    string                   `json:"description"`
	LaunchSettings *WorkspaceLaunchSettings `json:"launchSettings"`
	CreatedAt      time.Time                `json:"createdAt"`
	UpdatedAt      time.Time                `json:"updatedAt"`
}

// FromModel DO → 响应：launch_settings 解析失败兜底 nil（不阻塞列表加载）。
func (WorkspaceResponseData) FromModel(r *model.Workspace) WorkspaceResponseData {
	out := WorkspaceResponseData{
		ID:          r.ID,
		Name:        r.Name,
		Dir:         r.Dir,
		Description: r.Description,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
	if r.LaunchSettings != "" {
		var ls WorkspaceLaunchSettings
		if err := json.Unmarshal([]byte(r.LaunchSettings), &ls); err == nil {
			out.LaunchSettings = &ls
		}
	}
	return out
}
