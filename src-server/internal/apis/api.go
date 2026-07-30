// Package apis 提供 controller/service 的公共基类与链式依赖注入，统一所有 API 模块的运行期装配。
//
// 业务 controller/service 以「嵌入」方式获得该能力：
//
//	type Workspace  struct { apis.Api }     // controller
//	type Workspace  struct { apis.Service } // service
package apis

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"go.uber.org/zap"
	"gorm.io/gorm"

	vd "github.com/bytedance/go-tagexpr/v2/validator"

	"we-claude-terminal/go-server/internal/global"
)

// Api 是所有 controller 的基类：缓存单次请求的 ctx / Orm / Logger 与链式累积错误。
type Api struct {
	Context *gin.Context
	Orm     *gorm.DB
	Logger  *zap.Logger
	Errors  error
}

// AddError 累积链式调用中的错误（多条用 "; " 拼接），链尾统一读取 .Errors 判断。
func (api *Api) AddError(err error) {
	if err == nil {
		return
	}
	if api.Logger != nil {
		api.Logger.Sugar().Error(err)
	}
	if api.Errors == nil {
		api.Errors = err
	} else {
		api.Errors = fmt.Errorf("%v; %w", api.Errors, err)
	}
}

// MakeContext 注入 gin 上下文、全局 logger 与 sqlite 句柄（链式第一步，须先于 Bind/Validate/MakeService）。
// 三者皆为进程级单例、无按请求变化，故合并为一步；事务场景如需换 DB，可在链中之后直接覆写 api.Orm。
func (api *Api) MakeContext(ctx *gin.Context) *Api {
	api.Context = ctx
	api.Logger = global.Logger
	api.Orm = global.SqliteDB
	return api
}

// Bind 绑定请求参数：默认 binding.JSON（全 POST 约定）；显式传 binding.Query/Form 可覆盖、传 nil 走 URI。
// Body 类绑定用 ShouldBindBodyWith 缓存原始 body，支持一次请求内重复 Bind。
func (api *Api) Bind(data interface{}, bindings ...binding.Binding) *Api {
	var b binding.Binding = binding.JSON // 显式声明为 Binding 接口，以便下面 type-assert 到 BindingBody
	if len(bindings) > 0 {
		b = bindings[0]
	}
	var err error
	switch b {
	case nil:
		err = api.Context.ShouldBindUri(data)
	default:
		if body, ok := b.(binding.BindingBody); ok {
			err = api.Context.ShouldBindBodyWith(data, body)
		} else {
			err = api.Context.ShouldBindWith(data, b)
		}
	}
	if err != nil && err.Error() == "EOF" {
		err = nil // 无 body（如空 POST 的 getList），忽略
	}
	if err != nil {
		api.AddError(fmt.Errorf("参数解析失败: %w", err))
	}
	return api
}

// Validate 走 go-tagexpr vd：仅对带 vd tag 的字段生效（常规 DTO 用 binding tag，此处为 no-op）。
// 用于跨字段/复杂表达式校验（如二选一必填），错误格式 "参数校验失败, field: <f>, msg: <中文>"。
func (api *Api) Validate(data interface{}) *Api {
	vd.SetErrorFactory(func(fieldSelector, msg string) error {
		return fmt.Errorf("参数校验失败, field: %s, msg: %s", fieldSelector, msg)
	})
	if err := vd.Validate(data); err != nil {
		api.AddError(err)
	}
	return api
}

// MakeService 把 Api 的 ctx/Orm/Logger 灌入 service 基类（controller → service 的依赖桥）。
func (api *Api) MakeService(svc *Service) *Api {
	svc.Context = api.Context
	svc.Orm = api.Orm
	svc.Logger = api.Logger
	return api
}

// JsonOK 成功响应（转调本包 JsonOK）。
func (api Api) JsonOK(data interface{}) { JsonOK(api.Context, data) }

// JsonFail 失败响应（err.Error() 作为 msg，code=失败）。
func (api Api) JsonFail(err error) { JsonFail(api.Context, err) }

// JsonPageOK 是 controller 侧便捷方法（用 api.Context）。
func (api Api) JsonPageOK(list interface{}, pageInfo *PageInfo) {
	JsonPageOK(api.Context, list, pageInfo)
}
