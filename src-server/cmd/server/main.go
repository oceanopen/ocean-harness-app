// Package main 是 HTTP 本地服务的入口（gin 实现）。
//
// 由 Tauri 应用（Rust 侧 src-tauri/src/shared/http_server.rs）在启动时拉起：
//   - test 模式：先 `go build` 编译出二进制，再 spawn（持有真正的服务进程 handle）
//   - build 模式：spawn 随包分发的 sidecar 二进制
//   - dev 模式：本地 air 自测
//
// test/build 都 spawn 二进制而非 `go run`：避免孤儿进程。
//
// 配置优先级：环境变量（GO_SERVER_*）> 配置文件（-config 指定的 yaml，可选）。
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"we-claude-terminal/go-server/internal/config"
	"we-claude-terminal/go-server/internal/global"
	"we-claude-terminal/go-server/internal/initialize"
	"we-claude-terminal/go-server/internal/router"
)

func main() {
	// 1) 加载 + 校验配置（优先级：环境变量 > 配置文件；任一不合规即 log.Fatalf 退出）。
	cfg := config.MustLoadConfig()
	// 配置加载后立即写入全局：service 层读 global.Config.Mode 等，须在路由启动前就位。
	global.Config = cfg

	// 2) 初始化日志（zap）：日志目录来自环境变量，文件 + 控制台双写。
	initialize.MustInitZapLogger(cfg)

	// 3) 初始化 sqlite：数据目录来自环境变量。
	initialize.MustInitSQLite(cfg)

	// 3.5) 自动迁移：执行 embed 进二进制的未应用 SQL 迁移（goose），失败即 Fatal。
	initialize.MustRunMigrations(context.Background())

	// 4) gin 默认输出桥接到 zap，使 gin 日志也走文件 + 控制台。
	initialize.InitGinLoggerWriter()

	// 5) 服务启动前打印完整环境变量信息（用 zap，文件 + 控制台都有）。
	printRuntimeConfig(cfg)

	// 6) 组装路由（仅 /api/baseInfo/getServerRunInfo，无登录/鉴权）。
	engine := router.SetupRouter()

	// 7) 优雅退出：Rust 在应用退出时发 SIGTERM，监听后关闭连接。
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: engine}
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		global.Logger.Info("http-server shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	// 8) 启动后用 zap 打印服务地址（文件 + 控制台都有）。
	global.Logger.Info("http-server listening", zap.String("addr", addr), zap.String("mode", gin.Mode()))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		global.Logger.Fatal("serve failed", zap.Error(err))
	}
	global.Logger.Info("http-server stopped")
}

// printRuntimeConfig 打印当前运行配置（均来自环境变量），便于排障。
func printRuntimeConfig(cfg *config.Config) {
	global.Logger.Info("runtime config",
		zap.String("mode", cfg.Mode),
		zap.Int("port", cfg.Port),
		zap.String("logDir", cfg.LogDir),
		zap.String("sqliteDir", cfg.SqliteDir),
	)
}
