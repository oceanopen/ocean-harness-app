// Package controller 处理 HTTP 请求：参数解析/校验、调 service、统一响应封装。
package controller

import (
	"github.com/gin-gonic/gin"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/types"
	"we-claude-terminal/go-server/internal/service"
)

// BaseInfo 对应 /api/baseInfo 命名空间下的接口（系统信息查询，非 tracker 业务域，保留 GET）。
type BaseInfo struct {
	apis.Api
}

// GetServerRunInfo GET /api/baseInfo/getServerRunInfo：返回系统信息 + 服务运行信息。
func (api BaseInfo) GetServerRunInfo(ctx *gin.Context) {
	req := &types.ServerRunInfoRequest{}
	svc := service.BaseInfo{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.GetServerRunInfo(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}
