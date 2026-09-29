package controller

import (
	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/service"
)

// ImBot 对应 /api/imBot 命名空间下的接口（IM 渠道数字人 bot CRUD + 重启连接）。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type ImBot struct {
	apis.Api
}

// GetList POST /api/imBot/getList：返回全部 bot（凭据明文回显 + 运行态合并，本地数据语义）。
func (api ImBot) GetList(ctx *gin.Context) {
	req := &types.ImBotGetListRequest{}
	svc := service.ImBot{}
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

// GetInfo POST /api/imBot/getInfo：返回单个 bot。
func (api ImBot) GetInfo(ctx *gin.Context) {
	req := &types.ImBotGetInfoRequest{}
	svc := service.ImBot{}
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

// Create POST /api/imBot/create：新增 bot（校验 + 落库 + 启用即拉起连接）。
func (api ImBot) Create(ctx *gin.Context) {
	req := &types.ImBotCreateRequest{}
	svc := service.ImBot{}
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

// Update POST /api/imBot/update：更新 bot（secret 留空沿用原值；配置变更即热更新连接）。
func (api ImBot) Update(ctx *gin.Context) {
	req := &types.ImBotUpdateRequest{}
	svc := service.ImBot{}
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

// Delete POST /api/imBot/delete：物理删除 bot + 级联删会话映射 + 停连接。
func (api ImBot) Delete(ctx *gin.Context) {
	req := &types.ImBotDeleteRequest{}
	svc := service.ImBot{}
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

// Restart POST /api/imBot/restart：以当前配置重连（被踢/挂死后的人工恢复）。
func (api ImBot) Restart(ctx *gin.Context) {
	req := &types.ImBotRestartRequest{}
	svc := service.ImBot{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.Restart(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}

// ProvisionBegin POST /api/imBot/provisionBegin：开新扫码授权会话（返回二维码内容 + 轮询节奏）。
func (api ImBot) ProvisionBegin(ctx *gin.Context) {
	req := &types.ImBotProvisionBeginRequest{}
	svc := service.ImBot{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.ProvisionBegin()
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// ProvisionPoll POST /api/imBot/provisionPoll：轮询扫码结果（惰性单飞；connected 即激活完成）。
func (api ImBot) ProvisionPoll(ctx *gin.Context) {
	req := &types.ImBotProvisionPollRequest{}
	svc := service.ImBot{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.ProvisionPoll(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// ProvisionCancel POST /api/imBot/provisionCancel：取消扫码会话。
func (api ImBot) ProvisionCancel(ctx *gin.Context) {
	req := &types.ImBotProvisionCancelRequest{}
	svc := service.ImBot{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.ProvisionCancel(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}
