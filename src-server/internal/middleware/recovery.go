// Package middleware 存放 gin 中间件。
package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"ocean-harness/src-server/internal/apis"
	"ocean-harness/src-server/internal/global"
)

// Recovery 捕获 panic，用 zap 记录堆栈，并以统一响应结构返回 500。
func Recovery() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				global.Logger.Error("panic recovered",
					zap.Any("error", r),
					zap.String("stack", string(debug.Stack())),
					zap.String("path", ctx.Request.URL.Path),
				)
				ctx.AbortWithStatusJSON(http.StatusInternalServerError, apis.Response{
					Code: apis.RESPONSE_CODE_ERROR,
					Msg:  "internal server error",
					Data: nil,
				})
			}
		}()
		ctx.Next()
	}
}
