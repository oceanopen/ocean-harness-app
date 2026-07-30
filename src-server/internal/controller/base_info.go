// Package controller 处理 HTTP 请求：参数解析/校验、调 service、统一响应封装。
package controller

import (
	"github.com/gin-gonic/gin"

	"we-claude-terminal/go-server/internal/dal/types"
	"we-claude-terminal/go-server/internal/response"
	"we-claude-terminal/go-server/internal/service"
)

// BaseInfoController 对应 /api/baseInfo 命名空间下的接口。
type BaseInfoController struct {
	baseInfoService *service.BaseInfoService
}

// NewBaseInfoController 构造 BaseInfoController，注入依赖的 service。
func NewBaseInfoController() *BaseInfoController {
	return &BaseInfoController{
		baseInfoService: service.NewBaseInfoService(),
	}
}

// GetServerRunInfo GET /api/baseInfo/getServerRunInfo：返回系统信息 + 服务运行信息。
func (ctl *BaseInfoController) GetServerRunInfo(c *gin.Context) {
	data, err := ctl.baseInfoService.GetServerRunInfo(&types.ServerRunInfoRequest{})
	if err != nil {
		response.Fail(c, err.Error())
		return
	}
	response.OK(c, data)
}
