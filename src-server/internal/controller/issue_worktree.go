package controller

import (
	"github.com/gin-gonic/gin"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/types"
	"we-claude-terminal/go-server/internal/service"
)

// IssueWorktree 对应 /api/tracker/issueWorktree 命名空间下的接口（issue 开发流程 worktree 元数据）。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type IssueWorktree struct {
	apis.Api
}

// CreateWorktree POST /api/tracker/issueWorktree/createWorktree：为 issue 创建 worktree 记录（P1 桩：派生假路径，不真调 git）。
func (api IssueWorktree) CreateWorktree(ctx *gin.Context) {
	req := &types.IssueWorktreeCreateWorktreeRequest{}
	svc := service.IssueWorktree{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.CreateWorktree(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// RemoveWorktree POST /api/tracker/issueWorktree/removeWorktree：软删 worktree 记录（前置：前端已停 PTY）。
func (api IssueWorktree) RemoveWorktree(ctx *gin.Context) {
	req := &types.IssueWorktreeRemoveWorktreeRequest{}
	svc := service.IssueWorktree{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.RemoveWorktree(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}

// GetList POST /api/tracker/issueWorktree/getList：列某 issue 的 active worktree（前端作 worktreePath/worktreeId SSOT）。
func (api IssueWorktree) GetList(ctx *gin.Context) {
	req := &types.IssueWorktreeGetListRequest{}
	svc := service.IssueWorktree{}
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
