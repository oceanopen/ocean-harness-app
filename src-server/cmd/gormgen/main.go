// Package main 是 gorm/gen 代码生成器入口：连接当前 sqlite，按既有表结构生成 DO（PO 层）到 internal/dal。
//
//  1. 复用服务 initialize 序列（config → zap → sqlite → goose 迁移），确保库与 6 张业务表已就绪；
//  2. InitGen 装配生成器（输出目录 / 字段类型映射 / 命名策略）；
//  3. GenModelTracker 注册 6 张业务表（GenerateModelAs + ApplyBasic）；
//  4. G.Execute 落盘到 internal/dal（query/model 子包）。
//
// 运行：pnpm server:gorm:gen（等价 cd src-server && go run ./cmd/gormgen -config config/settings.dev.yaml）。
// 首次引入依赖需 GOPROXY=https://goproxy.cn,direct go mod tidy（见 README「gorm/gen 代码生成」）。
//
// 改表流程：改 migrations/*.sql → 跑一次服务（或本工具）触发迁移建表 → 重跑本工具重新生成 DO。
package main

import (
	"context"

	"gorm.io/gen"

	"we-claude-terminal/go-server/internal/config"
	"we-claude-terminal/go-server/internal/global"
	"we-claude-terminal/go-server/internal/initialize"
)

// G 是全局生成器：InitGen 装配、GenModel* 注册模型、main 末尾 Execute 落盘。
var G *gen.Generator

func main() {
	// 1) 复用服务初始化序列：加载配置（-config 指定 yaml，环境变量优先）→ zap → sqlite → 自动迁移（确保 6 表已建）。
	cfg := config.MustLoadConfig()
	global.Config = cfg
	initialize.MustInitZapLogger(cfg)
	initialize.MustInitSQLite(cfg)
	initialize.MustRunMigrations(context.Background())

	// 2) 装配生成器（输出目录 / 模式 / 字段映射 / 命名策略）。
	InitGen()

	// 3) 注册业务表：tracker 模块 6 张 + 本地仓库 1 张。
	GenModelTracker()
	GenModelLocalRepository()

	// 4) 落盘到 internal/dal（query/model 子包）。
	G.Execute()
}
