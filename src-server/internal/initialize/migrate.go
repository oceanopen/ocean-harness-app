package initialize

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"go.uber.org/zap"

	"ocean-harness/src-server/internal/global"
	"ocean-harness/src-server/internal/migrations"
)

// migrationsDir 是 embed.FS 内迁移文件所在子目录（与 embed.go 的 //go:embed 路径对应）。
const migrationsDir = "migrations"

// MustRunMigrations 在启动时自动执行未应用的 SQL 迁移（goose）。
//
// 设计要点：
//   - 复用 global.SqliteDB 的底层 *sql.DB，避免与 gorm 各开连接争 SQLite 写锁。
//   - 迁移文件通过 go:embed 嵌入二进制（migrations.FS），sidecar 分发无需外部 SQL。
//   - 失败即 Fatal 退出（Must 语义），杜绝 schema 不一致时服务继续启动。
//   - 仅向前 up 迁移；迁移文件只写 -- +goose Up 段。
//   - goose_db_version.tstamp 为 UTC 是预期行为：goose 的 sqlite3 方言用
//     datetime('now') 作列默认值，而 SQLite 无会话时区、该函数恒返回 UTC。tstamp 是
//     goose 内部审计字段（app 不读），UTC 无歧义故保持原样。
func MustRunMigrations(ctx context.Context) {
	sqlDB, err := global.SqliteDB.DB()
	if err != nil {
		global.Logger.Fatal("migrate get *sql.DB failed", zap.Error(err))
	}

	// embed.FS 里迁移文件在 "migrations" 子目录下；Provider 需要根在该目录的 fs.FS。
	migrationFS, err := fs.Sub(migrations.FS, migrationsDir)
	if err != nil {
		global.Logger.Fatal("migrate sub fs failed", zap.Error(err))
	}

	// Provider API（goose 官方推荐）：显式配置、无包级全局态、可测试。dialect=sqlite3
	// 既驱动迁移文件的 SQL 解析，也用于构造默认版本表 store；日志桥接到全局 zap。
	prov, err := goose.NewProvider(
		goose.DialectSQLite3, sqlDB, migrationFS,
		goose.WithLogger(newGooseLogger(global.Logger)),
	)
	if err != nil {
		global.Logger.Fatal("migrate new provider failed", zap.Error(err))
	}

	// 仅向前 up：执行所有未应用的迁移（失败即 Fatal，杜绝 schema 不一致时继续启动）。
	results, err := prov.Up(ctx)
	if err != nil {
		global.Logger.Fatal("migrate up failed", zap.Error(err))
	}

	version, err := prov.GetDBVersion(ctx)
	if err != nil {
		global.Logger.Fatal("migrate get version failed", zap.Error(err))
	}
	global.Logger.Info("sqlite migrations done",
		zap.Int64("version", version),
		zap.Int("applied", len(results)),
	)
}

// gooseLogger 实现 goose.Logger 接口，把 goose 的日志转发到全局 zap logger，
// 与 gin_writer.go 同构，使迁移日志进入 app.log / app.error.log / 控制台三路。
type gooseLogger struct{ logger *zap.Logger }

func newGooseLogger(l *zap.Logger) goose.Logger {
	return &gooseLogger{logger: l}
}

// Fatalf 兜底：library 模式下迁移失败由 UpContext 返回 err，已在 MustRunMigrations
// 里 Fatal；此处仅在 goose 内部异常路径触发时兜底退出。
func (g *gooseLogger) Fatalf(format string, v ...any) {
	g.logger.Fatal("goose fatal", zap.String("msg", fmt.Sprintf(format, v...)))
}

func (g *gooseLogger) Printf(format string, v ...any) {
	g.logger.Info("goose", zap.String("msg", fmt.Sprintf(format, v...)))
}

func (g *gooseLogger) Debugf(format string, v ...any) {
	g.logger.Debug("goose", zap.String("msg", fmt.Sprintf(format, v...)))
}
