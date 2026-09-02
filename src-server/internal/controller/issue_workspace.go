package controller

import (
	"github.com/gin-gonic/gin"

	"ocean-harness/src-server/internal/apis"
	"ocean-harness/src-server/internal/dal/types"
	"ocean-harness/src-server/internal/service"
)

// IssueWorkspace 对应 /api/issueWorkspace 命名空间下的接口（issue 运行工作空间初始化）。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type IssueWorkspace struct {
	apis.Api
}

// Init POST /api/issueWorkspace/init：受理初始化（后台异步执行），返回受理后状态
// （RUNNING；幂等命中时 SUCCESS；执行中重复触发返回当前进度）。
func (api IssueWorkspace) Init(ctx *gin.Context) {
	req := &types.IssueWorkspaceInitRequest{}
	svc := service.IssueWorkspace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Init(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Status POST /api/issueWorkspace/status：查询初始化进度（读状态文件派生，供前端轮询）。
func (api IssueWorkspace) Status(ctx *gin.Context) {
	req := &types.IssueWorkspaceStatusRequest{}
	svc := service.IssueWorkspace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Status(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Archive POST /api/issueWorkspace/archive：归档/取消工作空间（T3.2，两段式——force=false
// 仅安全检查返回警告不执行；force=true 删 {issueId}/ 目录 + 流转 issue 状态）。
func (api IssueWorkspace) Archive(ctx *gin.Context) {
	req := &types.IssueWorkspaceArchiveRequest{}
	svc := service.IssueWorkspace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Archive(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}
