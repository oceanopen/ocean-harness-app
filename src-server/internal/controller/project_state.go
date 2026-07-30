package controller

import (
	"github.com/gin-gonic/gin"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/types"
	"we-claude-terminal/go-server/internal/service"
)

// ProjectState 对应 /api/tracker/projectState 命名空间下的接口。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type ProjectState struct {
	apis.Api
}

// GetList POST /api/tracker/projectState/getList：返回某 project 下全部状态。
func (api ProjectState) GetList(ctx *gin.Context) {
	req := &types.ProjectStateGetListRequest{}
	svc := service.ProjectState{}
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

// Create POST /api/tracker/projectState/create：新建状态（sort_order 自算 + is_default 互斥）。
func (api ProjectState) Create(ctx *gin.Context) {
	req := &types.ProjectStateCreateRequest{}
	svc := service.ProjectState{}
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

// Update POST /api/tracker/projectState/update：更新状态（is_default 互斥）。
func (api ProjectState) Update(ctx *gin.Context) {
	req := &types.ProjectStateUpdateRequest{}
	svc := service.ProjectState{}
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

// Delete POST /api/tracker/projectState/delete：软删除状态（默认状态受保护）。
func (api ProjectState) Delete(ctx *gin.Context) {
	req := &types.ProjectStateDeleteRequest{}
	svc := service.ProjectState{}
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

// Reorder POST /api/tracker/projectState/reorder：批量重置 sort_order。
func (api ProjectState) Reorder(ctx *gin.Context) {
	req := &types.ProjectStateReorderRequest{}
	svc := service.ProjectState{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.Reorder(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}
