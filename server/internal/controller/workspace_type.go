package controller

import (
	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/service"
)

// WorkspaceType 对应 /api/tracker/workspaceType 命名空间下的接口。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type WorkspaceType struct {
	apis.Api
}

// GetList POST /api/tracker/workspaceType/getList：返回某 workspace 下全部类型。
func (api WorkspaceType) GetList(ctx *gin.Context) {
	req := &types.WorkspaceTypeGetListRequest{}
	svc := service.WorkspaceType{}
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

// GetInfo POST /api/tracker/workspaceType/getInfo：返回单个类型。
func (api WorkspaceType) GetInfo(ctx *gin.Context) {
	req := &types.WorkspaceTypeGetInfoRequest{}
	svc := service.WorkspaceType{}
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

// Create POST /api/tracker/workspaceType/create：创建类型（sort_order 后端自算）。
func (api WorkspaceType) Create(ctx *gin.Context) {
	req := &types.WorkspaceTypeCreateRequest{}
	svc := service.WorkspaceType{}
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

// Update POST /api/tracker/workspaceType/update：更新类型（name/color/description）。
func (api WorkspaceType) Update(ctx *gin.Context) {
	req := &types.WorkspaceTypeUpdateRequest{}
	svc := service.WorkspaceType{}
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

// Delete POST /api/tracker/workspaceType/delete：删除类型（引用该类型的 issue 置为未分类）。
func (api WorkspaceType) Delete(ctx *gin.Context) {
	req := &types.WorkspaceTypeDeleteRequest{}
	svc := service.WorkspaceType{}
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
