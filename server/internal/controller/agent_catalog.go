package controller

import (
	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/service"
)

// AgentCatalog 对应 /api/agentCatalog 命名空间下的接口（ACP agent 目录投影，只读）。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type AgentCatalog struct {
	apis.Api
}

// GetList POST /api/agentCatalog/getList：返回全部 catalog 条目（含 enabled 标记）。
func (api AgentCatalog) GetList(ctx *gin.Context) {
	req := &types.AgentCatalogGetListRequest{}
	svc := service.AgentCatalog{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.GetList()
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}
