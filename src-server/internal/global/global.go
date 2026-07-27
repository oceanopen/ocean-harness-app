// Package global 存放进程级单例：配置、zap logger、sqlite DB。
package global

import (
	"go.uber.org/zap"
	"gorm.io/gorm"

	"we-claude-terminal/go-server/internal/config"
)

var (
	// Config 为启动期从环境变量加载的配置（main 中 MustLoad 后赋值）。
	Config *config.Config

	// Logger 为全局 zap logger（初始化后与 zap.L()/zap.S() 等价）。
	Logger *zap.Logger

	// SqliteDB 为 sqlite 的 gorm 句柄。命名带 Sqlite 前缀，
	// 与未来可能引入的其他 DB（如远端 MySQL）区分。
	SqliteDB *gorm.DB
)
