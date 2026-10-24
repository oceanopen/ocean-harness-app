package controller

import (
	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/service"
)

/*
 * ProjectType 对应 /api/project/type 命名空间下的接口。
 * 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
 */
type ProjectType struct {
	apis.Api
}

// GetList POST /api/project/type/getList：返回某 workspace 下全部类型。
func (api ProjectType) GetList(ctx *gin.Context) {
	req := &types.ProjectTypeGetListRequest{}
	svc := service.ProjectType{}
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

// GetInfo POST /api/project/type/getInfo：返回单个类型。
func (api ProjectType) GetInfo(ctx *gin.Context) {
	req := &types.ProjectTypeGetInfoRequest{}
	svc := service.ProjectType{}
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

// Create POST /api/project/type/create：创建类型（sort_order 后端自算）。
func (api ProjectType) Create(ctx *gin.Context) {
	req := &types.ProjectTypeCreateRequest{}
	svc := service.ProjectType{}
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

// Update POST /api/project/type/update：更新类型（name/color/description）。
func (api ProjectType) Update(ctx *gin.Context) {
	req := &types.ProjectTypeUpdateRequest{}
	svc := service.ProjectType{}
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

// Delete POST /api/project/type/delete：删除类型（引用该类型的 issue 置为未分类）。
func (api ProjectType) Delete(ctx *gin.Context) {
	req := &types.ProjectTypeDeleteRequest{}
	svc := service.ProjectType{}
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
