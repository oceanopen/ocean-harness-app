package apis

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 响应码常量（中文 Go 生态主流约定：0 成功，非 0 失败）。
const (
	RESPONSE_CODE_SUCCESS = 0
	RESPONSE_CODE_ERROR   = 1
)

// Response 是所有接口的统一响应结构（{code, data, msg}）。
type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

// JsonOK 返回成功响应（HTTP 200 + code=0）。
func JsonOK(ctx *gin.Context, data interface{}) {
	ctx.JSON(http.StatusOK, Response{Code: RESPONSE_CODE_SUCCESS, Msg: "ok", Data: data})
}

/*
 * JsonFail 返回失败响应（HTTP 200 + code=1，err.Error() 作为 msg）。
 * 业务失败统一走 HTTP 200，由 code 区分；仅 panic 等系统级错误返回 5xx（见 middleware.Recovery）。
 */
func JsonFail(ctx *gin.Context, err error) {
	ctx.JSON(http.StatusOK, Response{Code: RESPONSE_CODE_ERROR, Msg: err.Error(), Data: nil})
}

// JsonFailWithCode 返回指定 code 的失败响应（msg 显式传入）。
func JsonFailWithCode(ctx *gin.Context, code int, msg string) {
	ctx.JSON(http.StatusOK, Response{Code: code, Msg: msg, Data: nil})
}

/*
 * JsonFailWithStatus 返回指定 HTTP 状态码的失败响应（SSE 端点订阅前置失败专用）：
 * EventSource 对非 200 fail the connection 不自动重连，借此把不可订阅落成终态而非
 * 3s 重连空转。业务 REST 约定仍是 JsonFail 的 200+code，不因本助手改变。
 */
func JsonFailWithStatus(ctx *gin.Context, httpStatus int, err error) {
	ctx.AbortWithStatusJSON(httpStatus, Response{Code: RESPONSE_CODE_ERROR, Msg: err.Error(), Data: nil})
}

/*
 * JsonPageOK 返回分页成功响应：data = {list, pageInfo}。
 * service 侧典型用法（gen 的 FindByPage 返 list + count）：FindByPage 取偏移/条数得 list 与 count，
 * 回填 pageInfo.TotalCount 后 return list, pageInfo, err。
 */
func JsonPageOK(ctx *gin.Context, list interface{}, pageInfo *PageInfo) {
	JsonOK(ctx, PageData{List: list, PageInfo: pageInfo})
}
