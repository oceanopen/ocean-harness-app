// Package mcputil 提供 MCP server 域的共用基础设施：McpTool 基类（依赖装配与校验）、
// MCP/McpFail 结果包装、Rust app_config 只读读取。各 MCP server（mcpservers 根目录下
// mcp_<server>.go 定义的 ocean_harness、后续 github 等）的工具 handler 均基于本包构建。
package mcputil

import (
	"context"
	"fmt"

	vd "github.com/bytedance/go-tagexpr/v2/validator"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/src-server/internal/apis"
	"ocean-harness/src-server/internal/global"
)

// McpTool 是所有 MCP 工具 handler 的基类（与 apis.Api 对 controller 的地位相同，适配 MCP
// 协议侧的调用链），业务工具以「嵌入」方式获得依赖装配与校验能力：
//
//	type McpOceanHarnessTool struct { mcputil.McpTool }
//
// 与 pros-admin-server 版的差异（本仓库无登录态，Orm/Logger 启动即就绪）：
//   - Context 用值类型 context.Context（pros 的 *context.Context 指针是历史包袱）；
//   - 无 SessionUser 字段——后续接入鉴权时，可在 MakeContext 内从 ctx 提取身份注入
//     新增字段（本方法即横切逻辑的唯一挂载点，工具 handler 无需逐个改动）。
type McpTool struct {
	Context context.Context
	Orm     *gorm.DB
	Logger  *zap.Logger
	Errors  error
}

// AddError 累积链式调用中的错误（与 apis.Api.AddError 同口径：多条用 "; " 拼接并即时打日志）。
func (mt *McpTool) AddError(err error) {
	if err == nil {
		return
	}
	if mt.Logger != nil {
		mt.Logger.Sugar().Error(err)
	}
	if mt.Errors == nil {
		mt.Errors = err
	} else {
		mt.Errors = fmt.Errorf("%v; %w", mt.Errors, err)
	}
}

// MakeContext 注入 MCP 请求 ctx、全局 logger 与 sqlite 句柄（链式第一步，与
// apis.Service.MakeContext 同源：三者皆为进程级单例，MCP 场景无 gin Context）。
func (mt *McpTool) MakeContext(ctx context.Context) *McpTool {
	mt.Context = ctx
	mt.Logger = global.Logger
	mt.Orm = global.SqliteDB
	return mt
}

// Validate 走 go-tagexpr vd（与 apis.Api.Validate 同口径的中文错误工厂；仅对带 vd tag 的
// 字段生效，常规必填由 SDK 反射推导的 InputSchema required 保证）。
func (mt *McpTool) Validate(data interface{}) *McpTool {
	vd.SetErrorFactory(func(fieldSelector, msg string) error {
		return fmt.Errorf("参数校验失败, field: %s, msg: %s", fieldSelector, msg)
	})
	if err := vd.Validate(data); err != nil {
		mt.AddError(err)
	}
	return mt
}

// MakeService 把 McpTool 的 ctx/Orm/Logger 灌入 service 基类（工具 handler 调用
// 业务 service 的依赖桥，与 apis.Api.MakeService 对应）。
func (mt *McpTool) MakeService(svc *apis.Service) *McpTool {
	svc.Context = mt.Context
	svc.Orm = mt.Orm
	svc.Logger = mt.Logger
	return mt
}
