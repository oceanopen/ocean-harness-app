package initialize

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"ocean-harness/src-server/internal/global"
)

// zapWriter 实现 io.Writer，把 gin 默认输出以 Info 级别写入 zap（文件 + 控制台）。
type zapWriter struct {
	logger *zap.Logger
}

// Write 实现 io.Writer。
func (w *zapWriter) Write(p []byte) (int, error) {
	w.logger.Info(string(p))
	return len(p), nil
}

// InitGinLoggerWriter 把 gin 的默认输出/错误输出桥接到 zap，
// 使 gin 自身日志（路由注册、启动 banner 等）也走统一的文件 + 控制台通道。
// DefaultWriter 与 DefaultErrorWriter 均桥接（默认仅 DefaultWriter 会漏掉错误流）。
func InitGinLoggerWriter() {
	w := &zapWriter{logger: global.Logger}
	gin.DefaultWriter = w
	gin.DefaultErrorWriter = w
}
