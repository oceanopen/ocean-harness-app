// Package middleware 存放 gin 中间件。
package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"we-claude-terminal/go-server/internal/global"
	"we-claude-terminal/go-server/internal/response"
)

// Recovery 捕获 panic，用 zap 记录堆栈，并以统一响应结构返回 500。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				global.Logger.Error("panic recovered",
					zap.Any("error", r),
					zap.String("stack", string(debug.Stack())),
					zap.String("path", c.Request.URL.Path),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, response.Response{
					Code: response.CodeError,
					Msg:  "internal server error",
					Data: nil,
				})
			}
		}()
		c.Next()
	}
}
