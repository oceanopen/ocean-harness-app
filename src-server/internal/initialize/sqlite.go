package initialize

import (
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"we-claude-terminal/go-server/internal/config"
	"we-claude-terminal/go-server/internal/global"
)

// sqlite 库文件名（落在 cfg.SqliteDir 目录下）。
const sqliteFileName = "server.db"

// MustInitSQLite 初始化 sqlite（gorm + 纯 Go 驱动 glebarez/sqlite，无 CGO，CI 交叉编译友好）。
// 数据目录来自环境变量（cfg.SqliteDir），库文件固定为 <dir>/server.db。
// 暂不建表/迁移（无业务模型），仅初始化连接 + ping 验活，为后续扩展预留。
func MustInitSQLite(cfg *config.Config) {
	dsn := filepath.Join(cfg.SqliteDir, sqliteFileName)

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Warn),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		global.Logger.Fatal("sqlite init failed", zap.Error(err))
	}

	sqlDB, err := db.DB()
	if err != nil {
		global.Logger.Fatal("sqlite get *sql.DB failed", zap.Error(err))
	}
	// sqlite 单文件写并发建议单连接，避免 "database is locked"。
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	if err := sqlDB.Ping(); err != nil {
		global.Logger.Fatal("sqlite ping failed", zap.Error(err))
	}

	global.SqliteDB = db
	global.Logger.Info("sqlite initialized", zap.String("dsn", dsn))
}
