package controller

import (
	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/service"
)

/*
 * WorkspaceIssue 对应 /api/tracker/workspaceIssue 命名空间下的接口。
 * 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
 */
type WorkspaceIssue struct {
	apis.Api
}

// GetList POST /api/tracker/workspaceIssue/getList：返回某 workspace 下 issue 列表（扁平，含类型）。
func (api WorkspaceIssue) GetList(ctx *gin.Context) {
	req := &types.WorkspaceIssueGetListRequest{}
	svc := service.WorkspaceIssue{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.GetList(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// GetInfo POST /api/tracker/workspaceIssue/getInfo：返回单个 issue（含类型）。
func (api WorkspaceIssue) GetInfo(ctx *gin.Context) {
	req := &types.WorkspaceIssueGetInfoRequest{}
	svc := service.WorkspaceIssue{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.GetInfo(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Create POST /api/tracker/workspaceIssue/create：创建 issue（默认状态 BACKLOG、sort_order 自算）。
func (api WorkspaceIssue) Create(ctx *gin.Context) {
	req := &types.WorkspaceIssueCreateRequest{}
	svc := service.WorkspaceIssue{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Create(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Update POST /api/tracker/workspaceIssue/update：更新 issue（stateCode 变化触发 completed_at 流转）。
func (api WorkspaceIssue) Update(ctx *gin.Context) {
	req := &types.WorkspaceIssueUpdateRequest{}
	svc := service.WorkspaceIssue{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Update(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Move POST /api/tracker/workspaceIssue/move：看板拖拽单卡移动（写 sortOrder + stateCode 流转 completed_at）。
func (api WorkspaceIssue) Move(ctx *gin.Context) {
	req := &types.WorkspaceIssueMoveRequest{}
	svc := service.WorkspaceIssue{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Move(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Delete POST /api/tracker/workspaceIssue/delete：删除 issue（级联清其仓库关联与 IM 绑定锚）。
func (api WorkspaceIssue) Delete(ctx *gin.Context) {
	req := &types.WorkspaceIssueDeleteRequest{}
	svc := service.WorkspaceIssue{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.Delete(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}
