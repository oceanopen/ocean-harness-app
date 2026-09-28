package controller

import (
	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/service"
)

// PluginMarketplace 对应 /api/pluginMarketplace 命名空间下的接口（插件市场投影与注册表操作）。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type PluginMarketplace struct {
	apis.Api
}

// GetList POST /api/pluginMarketplace/getList：返回全部插件市场投影（含插件与安装状态）。
func (api PluginMarketplace) GetList(ctx *gin.Context) {
	req := &types.PluginMarketplaceGetListRequest{}
	svc := service.PluginMarketplace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.GetList()
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Add POST /api/pluginMarketplace/add：注册插件市场（透传 claude CLI），返回最新列表投影。
func (api PluginMarketplace) Add(ctx *gin.Context) {
	req := &types.PluginMarketplaceAddRequest{}
	svc := service.PluginMarketplace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Add(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Update POST /api/pluginMarketplace/update：刷新插件市场，返回最新列表投影。
func (api PluginMarketplace) Update(ctx *gin.Context) {
	req := &types.PluginMarketplaceUpdateRequest{}
	svc := service.PluginMarketplace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.UpdateMarketplace(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Remove POST /api/pluginMarketplace/remove：注销插件市场（连带卸载其全部已装插件），
// 返回最新列表投影。
func (api PluginMarketplace) Remove(ctx *gin.Context) {
	req := &types.PluginMarketplaceRemoveRequest{}
	svc := service.PluginMarketplace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Remove(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Plugin 对应 /api/plugin 命名空间下的接口（按开发工具维度的插件安装操作）。
type Plugin struct {
	apis.Api
}

// operate 承载 Plugin 五个 action 的公共装配：执行后统一返回最新列表投影。
func (api Plugin) operate(action string, ctx *gin.Context) {
	req := &types.PluginOperationRequest{}
	svc := service.PluginMarketplace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Operate(action, req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Install POST /api/plugin/install：安装插件（user scope）。
func (api Plugin) Install(ctx *gin.Context) { api.operate("install", ctx) }

// Uninstall POST /api/plugin/uninstall：卸载插件。
func (api Plugin) Uninstall(ctx *gin.Context) { api.operate("uninstall", ctx) }

// Enable POST /api/plugin/enable：启用插件。
func (api Plugin) Enable(ctx *gin.Context) { api.operate("enable", ctx) }

// Disable POST /api/plugin/disable：禁用插件。
func (api Plugin) Disable(ctx *gin.Context) { api.operate("disable", ctx) }

// Update POST /api/plugin/update：更新插件到市场最新版本。
func (api Plugin) Update(ctx *gin.Context) { api.operate("update", ctx) }
