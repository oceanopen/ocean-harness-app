package controller

import (
	"github.com/gin-gonic/gin"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/types"
	"we-claude-terminal/go-server/internal/service"
)

// ProjectState 对应 /api/tracker/projectState 命名空间下的接口。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
//
// 状态管理无独立 CRUD/reorder 接口：状态随项目 create/update 全量提交。
// 本命名空间只保留 getList（项目状态数据）与 catalog（固定状态目录）。
type ProjectState struct {
	apis.Api
}

// GetList POST /api/tracker/projectState/getList：返回某 project 下全部状态。
func (api ProjectState) GetList(ctx *gin.Context) {
	req := &types.ProjectStateGetListRequest{}
	svc := service.ProjectState{}
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

// Catalog GET /api/tracker/projectState/catalog：返回固定状态目录（分组 + 子状态）。
// 全局常量、无入参，故省略 Bind/Validate。
func (api ProjectState) Catalog(ctx *gin.Context) {
	svc := service.ProjectState{}
	if err := api.MakeContext(ctx).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.GetCatalog()
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}
