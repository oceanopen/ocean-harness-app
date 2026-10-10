// Package main 是 HTTP 本地服务的入口（gin 实现）。
//
// 由 Tauri 应用（Rust 侧 app/src/shared/http_server.rs）在启动时拉起：
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

	"ocean-harness/server/internal/acpdoctor"
	"ocean-harness/server/internal/acpsession"
	"ocean-harness/server/internal/agentcatalog"
	"ocean-harness/server/internal/bot"
	"ocean-harness/server/internal/bot/wecom"
	"ocean-harness/server/internal/browser"
	"ocean-harness/server/internal/config"
	"ocean-harness/server/internal/global"
	"ocean-harness/server/internal/initialize"
	"ocean-harness/server/internal/router"
	"ocean-harness/server/internal/service"
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

	// 5.4) vendoring 两目录注入 agentcatalog（进程级一次）：bot driver / marketplace 的
	// vendored 自带 claude 一步式解析（ResolveVendoredClaudeBin）消费；agentcatalog 不
	// 反向依赖 config/global，由组装根在拉起 bot 前装配。
	agentcatalog.SetVendoredDirs(cfg.AcpResourcesDir, cfg.AcpAdaptersDir)

	// 5.5) ACP 会话域装配（T1.5）：启动清扫运行时锚（无 resume 语义，重启后旧锚全部失效
	// 归零），随后经 global.AcpSessions 承接 issue 级会话生命周期（ensure/prompt/respond/
	// discard）与 SSE 事件流；vendored 目录语义与 doctor 探测同源。先于 bot 装配——
	// supervisor 装配引擎路由时消费本实例。
	acpSessions, err := acpsession.NewManager(global.SqliteDB, acpsession.Dirs{
		Resources: cfg.AcpResourcesDir,
		Adapters:  cfg.AcpAdaptersDir,
	}, cfg.Port, global.Logger)
	if err != nil {
		global.Logger.Fatal("acp-session manager init failed", zap.Error(err))
	}
	global.AcpSessions = acpSessions

	// 5.55) 浏览器会话域装配（T2.1）：懒启动——构造即返回（不拉引擎），构造内含残留
	// 引擎孤儿清扫；per-profile 引擎随首次操作 ensure 拉起（D4 用完即释放）。
	// vendored 目录语义与 acp 会话域同源；先于路由装配——REST/SSE 面经 global.Browser 消费。
	global.Browser = browser.NewManager(browser.Dirs{
		Resources: cfg.AcpResourcesDir,
		Adapters:  cfg.AcpAdaptersDir,
	}, global.Logger)

	// 5.6) bot 运行时装配：注册企微适配器工厂 + 拉起全部启用 bot（WS 长连接后台运行，
	// 不阻塞 HTTP 启动；连接状态经 /api/imBot/getList 投影呈现）。
	global.BotSupervisor = bot.NewSupervisor(global.SqliteDB, cfg.Port, acpSessions, global.Logger)
	global.BotSupervisor.Register(wecom.Factory{})
	global.BotSupervisor.StartEnabled()
	// 扫码授权激活回调：凭据落库 + 拉连接（secret 仅经此路径入 sqlite，不回前端不进日志）。
	wecom.SetProvisionActivator(service.ProvisionActivateBot)

	// 5.7) doctor 启动后台探测：对全部 enabled agent 条目串行跑真实握手，把结论填进
	// 进程内缓存（重启即回到 unknown 由其重填）。非阻塞，不延迟 HTTP 监听；结论经
	// /api/doctor/getInfo 投影供前端引导。
	acpdoctor.RunStartupProbe(acpdoctor.Dirs{
		Resources: cfg.AcpResourcesDir,
		Adapters:  cfg.AcpAdaptersDir,
	}, global.Logger)

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
		// 先断 bot 渠道连接 + 取消在途回合（杀 claude 子进程），再收敛浏览器引擎/浏览器
		// 进程树，再收敛 ACP 会话（关全部 agent 进程 + 关停 SSE 订阅——不先断，SSE 长连
		// 会拖住下面的 Shutdown），再关 HTTP。
		global.BotSupervisor.StopAll()
		global.Browser.StopAll()
		global.AcpSessions.StopAll()
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
