package controller

import (
	"github.com/gin-gonic/gin"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/types"
	"we-claude-terminal/go-server/internal/service"
)

// Project 对应 /api/tracker/project 命名空间下的接口。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type Project struct {
	apis.Api
}

// GetList POST /api/tracker/project/getList：返回某 workspace 下全部项目。
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

// GetInfo POST /api/tracker/project/getInfo：返回单个项目。
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

// Create POST /api/tracker/project/create：创建项目（事务内种 5 默认状态 + 回填 default_state_id）。
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

// Update POST /api/tracker/project/update：更新项目（name/description/emoji）。
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

// Delete POST /api/tracker/project/delete：软删除项目（级联清其下 state/issue）。
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
