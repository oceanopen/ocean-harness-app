// Package global 存放进程级单例：配置、zap logger、sqlite DB。
package global

import (
	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/acpsession"
	"ocean-harness/server/internal/bot"
	"ocean-harness/server/internal/browser"
	"ocean-harness/server/internal/config"
)

var (
	// Config 为启动期从环境变量加载的配置（main 中 MustLoad 后赋值）。
	Config *config.Config

	// Logger 为全局 zap logger（初始化后与 zap.L()/zap.S() 等价）。
	Logger *zap.Logger

	// SqliteDB 为 sqlite 的 gorm 句柄。命名带 Sqlite 前缀，
	// 与未来可能引入的其他 DB（如远端 MySQL）区分。
	SqliteDB *gorm.DB

	// BotSupervisor 为 IM bot 运行时单例（main 装配：注册渠道工厂 + StartEnabled）。
	// service 层经此做 create/update/delete 后的连接热更新。
	BotSupervisor *bot.Supervisor

	// AcpSessions 为 ACP 会话域运行时单例（main 装配：启动清扫锚点 + SIGTERM StopAll）。
	// service 层经此做会话受理/操作/订阅（T1.5）；issue 删除经此级联 Discard。
	AcpSessions *acpsession.Manager

	// Browser 为浏览器会话域运行时单例（main 装配：懒启动构造即返回，构造内含残留
	// 引擎孤儿清扫；SIGTERM StopAll）。service 层经此做 per-profile 引擎会话操作与
	// SSE 投影订阅（T2.1）。
	Browser *browser.Manager
)
