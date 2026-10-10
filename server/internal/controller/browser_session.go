package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/global"
	"ocean-harness/server/internal/service"
)

// BrowserSession 对应 /api/browserSession 命名空间下的接口（浏览器会话域：会话/页面
// 操作 + SSE 投影事件流）。嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/
// MakeService）与 JsonOK/JsonFail。
type BrowserSession struct {
	apis.Api
}

// GetInfo POST /api/browserSession/getInfo：当前全量会话投影快照（轮询读端）。
func (api BrowserSession) GetInfo(ctx *gin.Context) {
	req := &types.BrowserGetInfoRequest{}
	svc := service.BrowserSession{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(svc.GetInfo())
}

// Ensure POST /api/browserSession/ensure：拉起/复用 profile 会话（同步等就绪，headless
// D10 覆盖）。
func (api BrowserSession) Ensure(ctx *gin.Context) {
	req := &types.BrowserEnsureRequest{}
	svc := service.BrowserSession{}
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

// Close POST /api/browserSession/close：释放会话（幂等）。
func (api BrowserSession) Close(ctx *gin.Context) {
	req := &types.BrowserCloseRequest{}
	svc := service.BrowserSession{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.Close(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}

// Navigate POST /api/browserSession/navigate：当前页打开地址（引擎结果原样透传）。
func (api BrowserSession) Navigate(ctx *gin.Context) {
	req := &types.BrowserNavigateRequest{}
	svc := service.BrowserSession{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Navigate(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Screenshot POST /api/browserSession/screenshot：截取当前页（ImageContent base64 与
// 落盘路径并存，供面板轮询伪实时）。
func (api BrowserSession) Screenshot(ctx *gin.Context) {
	req := &types.BrowserScreenshotRequest{}
	svc := service.BrowserSession{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Screenshot(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Analyze POST /api/browserSession/analyze：即时受理「AI 分析此页」入 issue 绑定的
// ACP 会话（会话缺失/回合进行中即中文报错，不排队）。
func (api BrowserSession) Analyze(ctx *gin.Context) {
	req := &types.BrowserAnalyzeRequest{}
	svc := service.BrowserSession{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.Analyze(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}

// Events GET /api/browserSession/events：SSE 投影事件流（无 query 绑定——会话后端全局
// 单源，全部 profile 的 snapshot/sessions 帧同一流，前端无需按 profile 建连）。建连首帧
// 即当前全量快照（seq 连续，杜绝快照与增量帧之间的缝隙）；帧协议见 browser.Frame。
// 断连/退订/服务收尾（hub 关停关闭通道）任一发生即返回。长驻期间持有 *gin.Context 是
// 安全的——gin 仅在 handler 返回后回收复用。
func (api BrowserSession) Events(ctx *gin.Context) {
	api.MakeContext(ctx)
	frames, cancel := global.Browser.Subscribe()
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
