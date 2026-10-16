package controller

import (
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/browser"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/global"
	"ocean-harness/server/internal/service"
)

/*
 * BrowserSession 对应 /api/browserSession 命名空间下的接口（浏览器会话域：会话/页面操作 + SSE 投影事件流）。
 * 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/ MakeService）与 JsonOK/JsonFail。
 */
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

/*
 * Ensure POST /api/browserSession/ensure：
 * 拉起/复用 profile 会话（同步等就绪，headless D10 覆盖）。
 */
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

/*
 * Close POST /api/browserSession/close：
 * 释放会话（幂等），响应携带释放后的投影行（收尾前已捕获真实当前页，供前端恢复现场）。
 */
func (api BrowserSession) Close(ctx *gin.Context) {
	req := &types.BrowserCloseRequest{}
	svc := service.BrowserSession{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Close(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
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

/*
 * Analyze POST /api/browserSession/analyze：
 * 即时受理「AI 分析此页」入 issue 绑定的 ACP 会话（会话缺失/回合进行中即中文报错，不排队）。
 */
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

/*
 * Events GET /api/browserSession/events：SSE 投影事件流（无 query 绑定——会话后端全局单源，
 * 全部 profile 的 snapshot/sessions 帧同一流，前端无需按 profile 建连）。建连首帧即当前全量快照（seq 连续，杜绝快照与增量帧之间的缝隙）；
 * 帧协议见 browser.Frame。断连/退订/服务收尾（hub 关停关闭通道）任一发生即返回。长驻期间持有 *gin.Context 是安全的——gin 仅在 handler 返回后回收复用。
 */
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

/*
 * liveSSEPingInterval 帧流 ping 周期：静态页零推帧时代理/客户端可能闲置断连，注释行防御性保活。
 */
const liveSSEPingInterval = 15 * time.Second

/*
 * liveSSEWriteTimeout SSE 单次写预算：客户端停滞读取时写超时结束流，防 handler 永久挂起。
 */
const liveSSEWriteTimeout = 10 * time.Second

/*
 * browserStreamReject 实时画面帧流的订阅前置判定：投影行状态不可订阅即返回原因错误
 * （空串=未知 profile；idle/failed=会话已关闭/引擎未运行；starting/ready 可订阅返回 nil）。
 * 错误经 streamReject 以 SSE reject 事件透出（真实原因前端可读），不再走 HTTP 状态码拒绝。
 */
func browserStreamReject(status browser.SessionStatus) error {
	switch status {
	case "":
		return errors.New("浏览器会话不存在")
	case browser.StatusIdle:
		return errors.New("浏览器会话已关闭")
	case browser.StatusFailed:
		return errors.New("浏览器引擎未运行")
	default:
		return nil
	}
}

/*
 * streamReject 订阅拒绝的 SSE 收敛出口：以合法 event-stream 返回 reject 事件（载荷真实
 * 中文原因）后终结——EventSource 据此置 failed 终态并拿到可排障的错误文本，替代拿不到
 * 状态码/响应体的 HTTP 拒绝路径。
 */
func (api BrowserSession) streamReject(err error) {
	writer := api.Context.Writer
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)
	api.Context.SSEvent("message", browser.LiveEvent{Type: browser.LiveEventRejected, Message: err.Error()})
	writer.Flush()
}

/*
 * Stream GET /api/browserSession/stream?profile=&targetId=：CDP 观察通道实时画面帧流（远程桌面语义，per-profile 单订——目标变更即重建，
 * 旧流经 done 感知重订）。事件五型 frame/target/error/closed/reject：error=自然断连可重订自愈，closed/reject=终态不重订；
 * 请求 ctx Done 即退订驱动 refs 减一（末订拆连接恢复 idle 计时）。长驻期间持有 *gin.Context 是安全的——gin 仅在 handler 返回后回收复用。
 */
func (api BrowserSession) Stream(ctx *gin.Context) {
	req := &types.BrowserStreamRequest{}
	svc := service.BrowserSession{}
	if err := api.MakeContext(ctx).Bind(req, binding.Query).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.streamReject(err)
		return
	}
	if err := browserStreamReject(global.Browser.Row(req.Profile).Status); err != nil {
		api.streamReject(err)
		return
	}
	stream, err := svc.Stream(req)
	if err != nil {
		api.streamReject(err)
		return
	}
	defer stream.Unsubscribe()

	writer := api.Context.Writer
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)

	hctx := api.Context.Request.Context()
	ping := time.NewTicker(liveSSEPingInterval)
	defer ping.Stop()
	/*
	 * 帧源是 keep-latest 单槽 mailbox：变更信号 channel 每次 deliver 关闭换新，select
	 * 直连——每帧至多一次唤醒，静态页零推帧时阻塞零开销。
	 */
	frameC := stream.FrameChanged()
	var seen uint64
	rc := http.NewResponseController(writer)
	writeEvent := func(evt browser.LiveEvent) {
		_ = rc.SetWriteDeadline(time.Now().Add(liveSSEWriteTimeout))
		api.Context.SSEvent("message", evt)
		writer.Flush()
	}
	/*
	 * 帧写侧节流（throttleMs>0 时生效，迷你预览档）：mailbox 只保留最新帧，
	 * 跳帧不丢末帧——节流窗口内到达的帧被跳过后，延迟定时器在窗口到期补写 mailbox 最新帧（gen 未推进则无写），静态页最后一帧不因节流永久滞留。
	 */
	throttle := max(time.Duration(req.ThrottleMs)*time.Millisecond, 0)
	var lastFrameAt time.Time
	var catchup *time.Timer
	var catchupC <-chan time.Time
	writeLatest := func() {
		if f, gen, ok := stream.LatestFrame(); ok && gen > seen {
			seen = gen
			writeEvent(browser.LiveEvent{
				Type:     browser.LiveEventFrame,
				Data:     base64.StdEncoding.EncodeToString(f.Data),
				Metadata: &f.Metadata,
			})
			lastFrameAt = time.Now()
		}
	}
	defer func() {
		if catchup != nil {
			catchup.Stop()
		}
	}()
	writeLatest() // 订阅建立即冲刷 mailbox 现有帧（attach 初始帧/重订恢复帧由此落流，静态页不再依赖后续推帧）
	for {
		select {
		case <-frameC:
			frameC = stream.FrameChanged()
			if throttle <= 0 {
				writeLatest()
				break
			}
			if elapsed := time.Since(lastFrameAt); elapsed >= throttle {
				writeLatest()
			} else if catchupC == nil {
				catchup = time.NewTimer(throttle - elapsed)
				catchupC = catchup.C
			}
		case <-catchupC:
			catchupC = nil
			writeLatest()
		case t := <-stream.Targets():
			writeEvent(browser.LiveEvent{Type: browser.LiveEventTarget, URL: t.URL, Title: t.Title, Closed: t.Closed})
			if t.Closed {
				writeEvent(browser.LiveEvent{Type: browser.LiveEventClosed, Reason: "页面已关闭"})
				return
			}
		case <-stream.Done():
			if reason := stream.CloseReason(); reason != "" {
				writeEvent(browser.LiveEvent{Type: browser.LiveEventClosed, Reason: reason})
			} else {
				writeEvent(browser.LiveEvent{Type: browser.LiveEventError, Message: "实时画面连接中断，正在自愈"})
			}
			return
		case <-ping.C:
			_ = rc.SetWriteDeadline(time.Now().Add(liveSSEWriteTimeout))
			_, _ = writer.WriteString(": ping\n\n")
			writer.Flush()
		case <-hctx.Done():
			return
		}
	}
}

/*
 * Input POST /api/browserSession/input：远程桌面输入分发（kind→CDP 映射在
 * service 层）。要求活跃观察面存在（未建实时画面即中文报错）；分发预算与失败口径随 DispatchInput。
 */
func (api BrowserSession) Input(ctx *gin.Context) {
	req := &types.BrowserInputRequest{}
	svc := service.BrowserSession{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.Input(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}
