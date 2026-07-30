package apis

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 响应码常量（中文 Go 生态主流约定：0 成功，非 0 失败）。
const (
	CodeSuccess = 0
	CodeError   = 1
)

// Response 是所有接口的统一响应结构（{code, data, msg}）。
type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

// JsonOK 返回成功响应（HTTP 200 + code=0）。
func JsonOK(ctx *gin.Context, data interface{}) {
	ctx.JSON(http.StatusOK, Response{Code: CodeSuccess, Msg: "ok", Data: data})
}

// JsonFail 返回失败响应（HTTP 200 + code=1，err.Error() 作为 msg）。
// 业务失败统一走 HTTP 200，由 code 区分；仅 panic 等系统级错误返回 5xx（见 middleware.Recovery）。
func JsonFail(ctx *gin.Context, err error) {
	ctx.JSON(http.StatusOK, Response{Code: CodeError, Msg: err.Error(), Data: nil})
}

// JsonFailWithCode 返回指定 code 的失败响应（msg 显式传入）。
func JsonFailWithCode(ctx *gin.Context, code int, msg string) {
	ctx.JSON(http.StatusOK, Response{Code: code, Msg: msg, Data: nil})
}
