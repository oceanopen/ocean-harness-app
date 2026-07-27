// Package response 提供统一的 HTTP 响应封装 {code, data, msg}。
//
// 所有 controller 通过 OK / Fail 返回该结构，前端按 code 判定业务结果。
package response

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

// OK 返回成功响应（HTTP 200 + code=0）。
func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{Code: CodeSuccess, Msg: "ok", Data: data})
}

// Fail 返回失败响应（HTTP 200 + code=1）。
// 业务失败统一走 HTTP 200，由 code 区分；仅 panic 等系统级错误返回 5xx（见 middleware.Recovery）。
func Fail(c *gin.Context, msg string) {
	FailWithCode(c, CodeError, msg)
}

// FailWithCode 返回指定 code 的失败响应。
func FailWithCode(c *gin.Context, code int, msg string) {
	c.JSON(http.StatusOK, Response{Code: code, Msg: msg, Data: nil})
}
