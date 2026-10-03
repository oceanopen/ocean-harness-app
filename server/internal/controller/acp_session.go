package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/global"
	"ocean-harness/server/internal/service"
)

// AcpSession 对应 /api/acpSession 命名空间下的接口（ACP 会话域：受理/操作 + SSE 事件流）。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type AcpSession struct {
	apis.Api
}

// Ensure POST /api/acpSession/ensure：幂等受理会话创建（异步，立即返回受理时刻快照）。
func (api AcpSession) Ensure(ctx *gin.Context) {
	req := &types.AcpSessionEnsureRequest{}
	svc := service.AcpSession{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Ensure(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// GetInfo POST /api/acpSession/getInfo：当前会话视图快照（轮询读端）。
func (api AcpSession) GetInfo(ctx *gin.Context) {
	req := &types.AcpSessionGetInfoRequest{}
	svc := service.AcpSession{}
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

// Prompt POST /api/acpSession/prompt：受理一轮回合（立即返回，终态经 SSE turnEnded）。
func (api AcpSession) Prompt(ctx *gin.Context) {
	req := &types.AcpSessionPromptRequest{}
	svc := service.AcpSession{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.Prompt(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}

// Cancel POST /api/acpSession/cancel：软取消当前回合（幂等）。
func (api AcpSession) Cancel(ctx *gin.Context) {
	req := &types.AcpSessionCancelRequest{}
	svc := service.AcpSession{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.Cancel(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}

// RespondPermission POST /api/acpSession/respondPermission：应答挂起权限审批。
func (api AcpSession) RespondPermission(ctx *gin.Context) {
	req := &types.AcpSessionRespondPermissionRequest{}
	svc := service.AcpSession{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.RespondPermission(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}

// RespondElicitation POST /api/acpSession/respondElicitation：应答挂起 elicitation。
func (api AcpSession) RespondElicitation(ctx *gin.Context) {
	req := &types.AcpSessionRespondElicitationRequest{}
	svc := service.AcpSession{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.RespondElicitation(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}

// Events GET /api/acpSession/events?issueId=...：SSE 事件流。建连首帧即当前快照（seq
// 连续，杜绝快照与增量帧之间的缝隙）；帧协议见 acpsession.Frame。断连/退订/服务收尾
// （hub 关停关闭通道）任一发生即返回。长驻期间持有 *gin.Context 是安全的——gin 仅在
// handler 返回后回收复用。
func (api AcpSession) Events(ctx *gin.Context) {
	req := &types.AcpSessionEventsRequest{}
	svc := service.AcpSession{}
	if err := api.MakeContext(ctx).Bind(req, binding.Query).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	frames, cancel := global.AcpSessions.Subscribe(req.IssueID)
	defer cancel()

	writer := api.Context.Writer
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)
	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				return
			}
			api.Context.SSEvent("message", frame)
			writer.Flush()
		case <-api.Context.Request.Context().Done():
			return
		}
	}
}
