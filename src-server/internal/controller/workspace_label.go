package controller

import (
	"github.com/gin-gonic/gin"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/types"
	"we-claude-terminal/go-server/internal/service"
)

// WorkspaceLabel 对应 /api/tracker/workspaceLabel 命名空间下的接口。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type WorkspaceLabel struct {
	apis.Api
}

// GetList POST /api/tracker/workspaceLabel/getList：返回某 workspace 下全部标签。
func (api WorkspaceLabel) GetList(ctx *gin.Context) {
	req := &types.WorkspaceLabelGetListRequest{}
	svc := service.WorkspaceLabel{}
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

// GetInfo POST /api/tracker/workspaceLabel/getInfo：返回单个标签。
func (api WorkspaceLabel) GetInfo(ctx *gin.Context) {
	req := &types.WorkspaceLabelGetInfoRequest{}
	svc := service.WorkspaceLabel{}
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

// Create POST /api/tracker/workspaceLabel/create：创建标签（sort_order 后端自算）。
func (api WorkspaceLabel) Create(ctx *gin.Context) {
	req := &types.WorkspaceLabelCreateRequest{}
	svc := service.WorkspaceLabel{}
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

// Update POST /api/tracker/workspaceLabel/update：更新标签（name/color/description）。
func (api WorkspaceLabel) Update(ctx *gin.Context) {
	req := &types.WorkspaceLabelUpdateRequest{}
	svc := service.WorkspaceLabel{}
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

// Delete POST /api/tracker/workspaceLabel/delete：软删除标签（级联清 issue 关联）。
func (api WorkspaceLabel) Delete(ctx *gin.Context) {
	req := &types.WorkspaceLabelDeleteRequest{}
	svc := service.WorkspaceLabel{}
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
