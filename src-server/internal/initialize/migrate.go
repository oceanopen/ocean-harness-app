package initialize

import (
	"context"
	"fmt"

	"github.com/pressly/goose/v3"
	"go.uber.org/zap"

	"we-claude-terminal/go-server/internal/global"
	"we-claude-terminal/go-server/internal/migrations"
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
func MustRunMigrations(ctx context.Context) {
	sqlDB, err := global.SqliteDB.DB()
	if err != nil {
		global.Logger.Fatal("migrate get *sql.DB failed", zap.Error(err))
	}

	// 指定迁移文件来源为 embed.FS（编译期内置）；日志桥接到全局 zap。
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(newGooseLogger(global.Logger))
	// 显式锁定 sqlite 方言：goose 默认全局 dialect 为 Postgres，
	// 不调用 SetDialect 会用 Postgres 方言的版本表 SQL 在 sqlite 上报 "near BY" 语法错。
	if err := goose.SetDialect("sqlite3"); err != nil {
		global.Logger.Fatal("migrate set dialect failed", zap.Error(err))
	}

	if err := goose.UpContext(ctx, sqlDB, migrationsDir); err != nil {
		global.Logger.Fatal("migrate up failed", zap.Error(err))
	}

	version, err := goose.GetDBVersion(sqlDB)
	if err != nil {
		global.Logger.Fatal("migrate get version failed", zap.Error(err))
	}
	global.Logger.Info("sqlite migrations done", zap.Int64("version", version))
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
