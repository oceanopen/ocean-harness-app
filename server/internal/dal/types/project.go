package types

import (
	"time"

	"ocean-harness/server/internal/dal/model"
)

// ProjectGetListRequest 是 POST /api/project/getList 的入参（当前无参，预留筛选位）。
type ProjectGetListRequest struct{}

// ProjectGetInfoRequest 是 POST /api/project/getInfo 的入参。
type ProjectGetInfoRequest struct {
	ID string `json:"id" binding:"required"`
}

/*
 * ProjectCreateRequest 是 POST /api/project/create 的入参：dir 为项目根目录（须为绝对路径，service 层校验可创建可写）；
 * localRepositoryIds 为关联本地仓库全量列表（无 diff 全量覆盖，空 = 清空）；launchSettings 形状复用工作空间启动设置 SSOT。
 */
type ProjectCreateRequest struct {
	Name               string                   `json:"name" binding:"required,max=100"`
	Dir                string                   `json:"dir" binding:"required"`
	Description        string                   `json:"description" binding:"omitempty,max=500"`
	Emoji              string                   `json:"emoji" binding:"omitempty,max=20"`
	LaunchSettings     *WorkspaceLaunchSettings `json:"launchSettings" binding:"omitempty"`
	LocalRepositoryIDs []int                    `json:"localRepositoryIds"`
}

// ProjectUpdateRequest 是 POST /api/project/update 的入参（launchSettings nil = 清空配置）。
type ProjectUpdateRequest struct {
	ID                 string                   `json:"id" binding:"required"`
	Name               string                   `json:"name" binding:"required,max=100"`
	Dir                string                   `json:"dir" binding:"required"`
	Description        string                   `json:"description" binding:"omitempty,max=500"`
	Emoji              string                   `json:"emoji" binding:"omitempty,max=20"`
	LaunchSettings     *WorkspaceLaunchSettings `json:"launchSettings" binding:"omitempty"`
	LocalRepositoryIDs []int                    `json:"localRepositoryIds"`
}

// ProjectDeleteRequest 是 POST /api/project/delete 的入参。
type ProjectDeleteRequest struct {
	ID string `json:"id" binding:"required"`
}

/*
 * ProjectResponseData 项目响应：launch_settings JSON 列反序列化为结构；repoIds 由 service 装配
 * （FromModel 不查库），扁平呈现形态对齐 workspace 域。
 */
type ProjectResponseData struct {
	ID                 string                   `json:"id"`
	Name               string                   `json:"name"`
	Dir                string                   `json:"dir"`
	Description        string                   `json:"description"`
	Emoji              string                   `json:"emoji"`
	LaunchSettings     *WorkspaceLaunchSettings `json:"launchSettings"`
	LocalRepositoryIDs []int                    `json:"localRepositoryIds"`
	CreatedAt          time.Time                `json:"createdAt"`
	UpdatedAt          time.Time                `json:"updatedAt"`
}

// FromModel DO → 响应：launch_settings 解析失败兜底 nil（不阻塞列表加载）。
func (ProjectResponseData) FromModel(r *model.Project) ProjectResponseData {
	return ProjectResponseData{
		ID:             r.ID,
		Name:           r.Name,
		Dir:            r.Dir,
		Description:    r.Description,
		Emoji:          r.Emoji,
		LaunchSettings: ParseLaunchSettings(r.LaunchSettings),
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}
