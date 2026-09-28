package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/marketplace"
)

// PluginMarketplace 对应 /api/pluginMarketplace 与 /api/plugin 命名空间下的业务逻辑。
// 无本地表：市场注册表与安装状态的 SSOT 恒为 claude 侧（~/.claude/plugins），本 service
// 只做实时投影（CLI 输出 × 市场清单扫描）与命令透传，界面与命令行天然一致。
type PluginMarketplace struct {
	apis.Service
}

// pluginOpMu 把「claude 状态文件单写者」约束下沉到服务端：本域全部入口（读投影 + 写操作）
// 经它串行，杜绝并发请求同时拉起 claude 进程读写 ~/.claude/plugins（gin 每请求一个
// goroutine，无此锁则刷新与「更新市场」等长操作可并发投影出中间态甚至倒序覆盖）。
var pluginOpMu sync.Mutex

// lock 串行入口（读投影同样加锁：投影也是一次 claude 进程批量读取）。
func (svc PluginMarketplace) lock() func() {
	pluginOpMu.Lock()
	return pluginOpMu.Unlock
}

// supportedCLIs 是本域支持安装操作的 CLI 开发工具清单（前端同款文案的单源）。
// 未来接入新 CLI：marketplace 包新增 <cli>.go 命令文件 + 此处登记即可。
const supportedCLI = "claude"

// GetList 返回全部插件市场投影（市场注册表 × 清单扫描 × 安装状态 join）。
func (svc PluginMarketplace) GetList() (types.PluginMarketplaceListResponseData, error) {
	defer svc.lock()()
	return svc.assembleList()
}

// Add 注册插件市场（source 透传 claude CLI），成功后回读最新投影。
// 重复添加由 claude CLI 报错（同名市场已存在），本层不重复校验。
// 本地目录形式先做存在性预检（~ 前缀展开主目录），给出比 CLI 更友好的中文错误。
func (svc PluginMarketplace) Add(req *types.PluginMarketplaceAddRequest) (types.PluginMarketplaceListResponseData, error) {
	defer svc.lock()()
	source := strings.TrimSpace(req.Source)
	if source == "" {
		return types.PluginMarketplaceListResponseData{}, errors.New("插件市场地址不能为空")
	}
	if strings.HasPrefix(source, "~") {
		if home, e := os.UserHomeDir(); e == nil {
			source = filepath.Join(home, source[1:])
		}
	}
	// 仅对「本地目录」形式预检（.json 文件路径与远程源交由 claude CLI 校验）。
	if strings.HasPrefix(source, "/") && !strings.HasSuffix(source, ".json") {
		if info, e := os.Stat(source); e != nil || !info.IsDir() {
			return types.PluginMarketplaceListResponseData{}, errors.New("目录不存在或不可访问：" + source)
		}
		if !marketplace.HasManifest(source) {
			return types.PluginMarketplaceListResponseData{}, errors.New("该目录下未找到 .claude-plugin/marketplace.json，不是有效的插件市场目录")
		}
	}
	if err := marketplace.AddMarketplace(source); err != nil {
		return types.PluginMarketplaceListResponseData{}, err
	}
	return svc.assembleList()
}

// Remove 注销插件市场（claude 会连带卸载该市场全部已装插件，前端确认弹窗已告知），
// 成功后回读最新投影。
func (svc PluginMarketplace) Remove(req *types.PluginMarketplaceRemoveRequest) (types.PluginMarketplaceListResponseData, error) {
	defer svc.lock()()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return types.PluginMarketplaceListResponseData{}, errors.New("插件市场名不能为空")
	}
	if err := marketplace.RemoveMarketplace(name); err != nil {
		return types.PluginMarketplaceListResponseData{}, err
	}
	return svc.assembleList()
}

// UpdateMarketplace 刷新插件市场（github/git 源重新拉取；本地源重校验清单），回读最新投影。
func (svc PluginMarketplace) UpdateMarketplace(req *types.PluginMarketplaceUpdateRequest) (types.PluginMarketplaceListResponseData, error) {
	defer svc.lock()()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return types.PluginMarketplaceListResponseData{}, errors.New("插件市场名不能为空")
	}
	if err := marketplace.UpdateMarketplace(name); err != nil {
		return types.PluginMarketplaceListResponseData{}, err
	}
	return svc.assembleList()
}

// Operate 按开发工具维度执行插件操作（action：install/uninstall/enable/disable/update）。
// cli 维度从第一天进入契约：非 claude 一律拒绝，为多 CLI 扩展留口。
func (svc PluginMarketplace) Operate(action string, req *types.PluginOperationRequest) (types.PluginMarketplaceListResponseData, error) {
	defer svc.lock()()
	cli := strings.TrimSpace(req.CLI)
	if cli != supportedCLI {
		return types.PluginMarketplaceListResponseData{}, fmt.Errorf("暂不支持的开发工具：%s（当前仅支持 %s）", cli, supportedCLI)
	}
	name := strings.TrimSpace(req.Name)
	mkName := strings.TrimSpace(req.Marketplace)
	if name == "" || mkName == "" {
		return types.PluginMarketplaceListResponseData{}, errors.New("插件名与市场名不能为空")
	}
	var err error
	switch action {
	case "install":
		err = marketplace.InstallPlugin(name, mkName)
	case "uninstall":
		err = marketplace.UninstallPlugin(name, mkName)
	case "enable":
		err = marketplace.EnablePlugin(name, mkName)
	case "disable":
		err = marketplace.DisablePlugin(name, mkName)
	case "update":
		err = marketplace.UpdatePlugin(name, mkName)
	default:
		return types.PluginMarketplaceListResponseData{}, fmt.Errorf("未知的插件操作：%s", action)
	}
	if err != nil {
		return types.PluginMarketplaceListResponseData{}, err
	}
	return svc.assembleList()
}

// assembleList 聚合三路数据为响应投影：
//  1. `claude plugin marketplace list --json` → 市场注册表（name/source/installLocation）
//  2. 逐市场扫描 installLocation 清单 → 描述/版本/插件目录 + supportedClis 探测
//  3. `claude plugin list --json` → 按 id（name@marketplace）join 安装/启用状态
//
// 单个市场清单扫描失败不中断整体列表（ScanError 字段承载原因，前端提示）。
func (svc PluginMarketplace) assembleList() (types.PluginMarketplaceListResponseData, error) {
	entries, err := marketplace.ListMarketplaces()
	if err != nil {
		return types.PluginMarketplaceListResponseData{}, err
	}
	installed, err := marketplace.ListInstalledPlugins()
	if err != nil {
		return types.PluginMarketplaceListResponseData{}, err
	}

	// 安装状态索引：id → 状态。同插件多 scope 时 user scope 优先（本域操作恒 user scope），
	// 无 user 记录时取首条如实展示（scope 字段透传，前端对非 user scope 的行禁用操作按钮）。
	type installState struct {
		installed bool
		enabled   bool
		version   string
		scope     string
	}
	states := make(map[string]installState, len(installed))
	for _, p := range installed {
		cur, ok := states[p.ID]
		if ok && cur.installed && p.Scope != "user" {
			continue
		}
		states[p.ID] = installState{installed: true, enabled: p.Enabled, version: p.Version, scope: p.Scope}
	}

	data := types.PluginMarketplaceListResponseData{
		Clis:         []string{supportedCLI},
		Marketplaces: make([]types.PluginMarketplaceResponseData, 0, len(entries)),
	}
	for _, e := range entries {
		sourceType, sourceDetail := e.SourceType()
		md := types.PluginMarketplaceResponseData{
			Name:            e.Name,
			SourceType:      sourceType,
			SourceDetail:    sourceDetail,
			InstallLocation: e.InstallLocation,
			SupportedClis:   marketplace.DetectSupportedClis(e.InstallLocation),
			Plugins:         []types.MarketplacePluginResponseData{},
		}
		mf, mfErr := marketplace.LoadManifest(e.InstallLocation)
		if mfErr != nil {
			md.ScanError = mfErr.Error()
			data.Marketplaces = append(data.Marketplaces, md)
			continue
		}
		md.Description = mf.Description
		md.Version = mf.Version
		for _, p := range mf.Plugins {
			pd := types.MarketplacePluginResponseData{
				Name:           p.Name,
				Source:         p.Source,
				SourceExternal: p.SourceExternal,
				Dir:            p.Dir,
				Description:    p.Description,
				Version:        p.Version,
				Components: types.PluginComponentsResponseData{
					Commands:   p.Components.Commands,
					Skills:     p.Components.Skills,
					Agents:     p.Components.Agents,
					Hooks:      p.Components.Hooks,
					McpServers: p.Components.McpServers,
				},
			}
			if st, ok := states[marketplace.PluginRef(p.Name, e.Name)]; ok {
				pd.Installed = st.installed
				pd.Enabled = st.enabled
				pd.Scope = st.scope
				pd.InstalledVersion = st.version
			}
			md.Plugins = append(md.Plugins, pd)
		}
		data.Marketplaces = append(data.Marketplaces, md)
	}
	return data, nil
}
