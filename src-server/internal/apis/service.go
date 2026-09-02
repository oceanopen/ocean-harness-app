package apis

import (
	"context"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/src-server/internal/global"
)

// Service 是所有 service 的基类：承载由 controller 经 MakeService 灌入的 ctx/Orm/Logger。
//
// Context 用标准 context.Context（*gin.Context 可赋值给它，反之不行），便于 service 在脱离 gin 的
// 场景（脚本/定时任务）复用——届时直接 new(Service).MakeContext(ctx) 即可装好三件依赖。
type Service struct {
	Context context.Context
	Orm     *gorm.DB
	Logger  *zap.Logger
}

// MakeContext 注入 ctx / 全局 logger / sqlite 句柄（仅用于脚本/定时任务等脱离 gin 的场景直接构造 service）。
// HTTP 场景下 Service 的字段由 controller 的 Api.MakeService 灌入，无需手动调本方法。
func (svc *Service) MakeContext(ctx context.Context) *Service {
	svc.Context = ctx
	svc.Logger = global.Logger
	svc.Orm = global.SqliteDB
	return svc
}

// MakeService 用于 service 内部调用其它 service 时透传依赖（事务内调子 service 常用）。
func (svc *Service) MakeService(target *Service) *Service {
	target.Context = svc.Context
	target.Orm = svc.Orm
	target.Logger = svc.Logger
	return svc
}
