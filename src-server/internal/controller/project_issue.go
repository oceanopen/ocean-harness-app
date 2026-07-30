package controller

import (
	"github.com/gin-gonic/gin"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/types"
	"we-claude-terminal/go-server/internal/service"
)

// ProjectIssue 对应 /api/tracker/projectIssue 命名空间下的接口。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type ProjectIssue struct {
	apis.Api
}

// GetList POST /api/tracker/projectIssue/getList：返回某 project 下 issue 列表（扁平，含 labels）。
func (api ProjectIssue) GetList(ctx *gin.Context) {
	req := &types.ProjectIssueGetListRequest{}
	svc := service.ProjectIssue{}
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

// GetInfo POST /api/tracker/projectIssue/getInfo：返回单个 issue（含 labels）。
func (api ProjectIssue) GetInfo(ctx *gin.Context) {
	req := &types.ProjectIssueGetInfoRequest{}
	svc := service.ProjectIssue{}
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

// Create POST /api/tracker/projectIssue/create：创建 issue（默认 state 取 project.default_state_id、sort_order 自算）。
func (api ProjectIssue) Create(ctx *gin.Context) {
	req := &types.ProjectIssueCreateRequest{}
	svc := service.ProjectIssue{}
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

// Update POST /api/tracker/projectIssue/update：更新 issue（stateId 变化触发 completed_at 流转）。
func (api ProjectIssue) Update(ctx *gin.Context) {
	req := &types.ProjectIssueUpdateRequest{}
	svc := service.ProjectIssue{}
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

// Delete POST /api/tracker/projectIssue/delete：软删除 issue（级联清其 label 关联）。
func (api ProjectIssue) Delete(ctx *gin.Context) {
	req := &types.ProjectIssueDeleteRequest{}
	svc := service.ProjectIssue{}
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
