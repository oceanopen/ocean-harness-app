package controller

import (
	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/service"
)

// Doctor 对应 /api/doctor 命名空间下的接口（ACP agent doctor 探测：受理 + 四态投影）。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type Doctor struct {
	apis.Api
}

// Check POST /api/doctor/check：受理探测（异步，立即返回受理清单与四态快照）。
func (api Doctor) Check(ctx *gin.Context) {
	req := &types.DoctorCheckRequest{}
	svc := service.Doctor{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Check(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// GetInfo POST /api/doctor/getInfo：返回全部 enabled 条目的当前四态快照（轮询读端）。
func (api Doctor) GetInfo(ctx *gin.Context) {
	req := &types.DoctorGetInfoRequest{}
	svc := service.Doctor{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.GetInfo()
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}
