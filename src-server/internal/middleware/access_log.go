// access_log.go 记录每个 HTTP 请求的访问日志到 zap（path / method / status / latency），按状态码分级。
// 仅打印不落库、不记录请求/响应体（本地旁路服务、访问量低，结构化字段足够排障）。
package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"we-claude-terminal/go-server/internal/global"
)

// AccessLog 记录每个请求的访问日志，按 HTTP 状态码分级：
//   - ≥500：Error（同时进 app.error.log，便于告警/排障）
//   - ≥400：Warn
//   - 其他：Info
//
// 须注册在 Recovery 之前：handler panic 被 Recovery 兜住返回 500 后，本中间件的后置日志
// 仍能执行，记录到 500 状态码与完整 latency（若注册在内层，panic 时访问日志会丢失）。
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		cost := time.Since(start)
		latency := cost.Truncate(time.Microsecond) // 截断到微秒，避免纳秒精度噪音，millisecond-毫秒, microsecond-微秒, nanosecond-纳秒

		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", status),
			zap.Duration("latency", latency),
		}
		switch {
		case status >= 500:
			global.Logger.Error("request", fields...)
		case status >= 400:
			global.Logger.Warn("request", fields...)
		default:
			global.Logger.Info("request", fields...)
		}
	}
}
