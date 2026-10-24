package controller

import (
	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/service"
)

/*
 * ProjectTask 对应 /api/project/task 命名空间下的接口。
 * 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
 */
type ProjectTask struct {
	apis.Api
}

// GetList POST /api/project/task/getList：返回某项目下任务列表（扁平，含类型）。
func (api ProjectTask) GetList(ctx *gin.Context) {
	req := &types.ProjectTaskGetListRequest{}
	svc := service.ProjectTask{}
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

// GetInfo POST /api/project/task/getInfo：返回单个任务（含类型）。
func (api ProjectTask) GetInfo(ctx *gin.Context) {
	req := &types.ProjectTaskGetInfoRequest{}
	svc := service.ProjectTask{}
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

// Create POST /api/project/task/create：创建任务（uuid v7 主键）。
func (api ProjectTask) Create(ctx *gin.Context) {
	req := &types.ProjectTaskCreateRequest{}
	svc := service.ProjectTask{}
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

// Update POST /api/project/task/update：更新任务业务字段。
func (api ProjectTask) Update(ctx *gin.Context) {
	req := &types.ProjectTaskUpdateRequest{}
	svc := service.ProjectTask{}
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

// Delete POST /api/project/task/delete：删除任务（级联清其仓库关联）。
func (api ProjectTask) Delete(ctx *gin.Context) {
	req := &types.ProjectTaskDeleteRequest{}
	svc := service.ProjectTask{}
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
