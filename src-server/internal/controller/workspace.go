package controller

import (
	"github.com/gin-gonic/gin"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/types"
	"we-claude-terminal/go-server/internal/service"
)

// Workspace 对应 /api/tracker/workspace 命名空间下的接口。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type Workspace struct {
	apis.Api
}

// GetList POST /api/tracker/workspace/getList：返回全部工作空间。
func (api Workspace) GetList(ctx *gin.Context) {
	req := &types.WorkspaceGetListRequest{}
	svc := service.Workspace{}
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

// GetInfo POST /api/tracker/workspace/getInfo：返回单个工作空间。
func (api Workspace) GetInfo(ctx *gin.Context) {
	req := &types.WorkspaceGetInfoRequest{}
	svc := service.Workspace{}
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

// Create POST /api/tracker/workspace/create：创建工作空间（slug 查重后插入）。
func (api Workspace) Create(ctx *gin.Context) {
	req := &types.WorkspaceCreateRequest{}
	svc := service.Workspace{}
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

// Update POST /api/tracker/workspace/update：更新工作空间。
func (api Workspace) Update(ctx *gin.Context) {
	req := &types.WorkspaceUpdateRequest{}
	svc := service.Workspace{}
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

// Delete POST /api/tracker/workspace/delete：删除工作空间（级联删其下 project 及关联）。
func (api Workspace) Delete(ctx *gin.Context) {
	req := &types.WorkspaceDeleteRequest{}
	svc := service.Workspace{}
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
