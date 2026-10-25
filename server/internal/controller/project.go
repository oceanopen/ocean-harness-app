package controller

import (
	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/service"
)

/*
 * Project 对应 /api/project 命名空间下的接口。
 * 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
 */
type Project struct {
	apis.Api
}

// GetList POST /api/project/getList：返回全部项目（id 倒序，含关联仓库 id）。
func (api Project) GetList(ctx *gin.Context) {
	req := &types.ProjectGetListRequest{}
	svc := service.Project{}
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

// GetInfo POST /api/project/getInfo：返回单个项目（含关联仓库 id）。
func (api Project) GetInfo(ctx *gin.Context) {
	req := &types.ProjectGetInfoRequest{}
	svc := service.Project{}
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

// Create POST /api/project/create：新建项目（目录校验 + 仓库关联落库）。
func (api Project) Create(ctx *gin.Context) {
	req := &types.ProjectCreateRequest{}
	svc := service.Project{}
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

// Update POST /api/project/update：更新项目（目录变更热更新关联 bot 连接）。
func (api Project) Update(ctx *gin.Context) {
	req := &types.ProjectUpdateRequest{}
	svc := service.Project{}
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

// Delete POST /api/project/delete：删除项目（级联任务/会话行/类型/仓库关联，bot 清列热更新）。
func (api Project) Delete(ctx *gin.Context) {
	req := &types.ProjectDeleteRequest{}
	svc := service.Project{}
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
