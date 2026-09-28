package marketplace

import (
	"encoding/json"
	"fmt"
)

// claude.go：claude plugin CLI 的命令构造与 JSON 输出解析。
// 命令口径与命令行完全一致（透传 CLI 能力）；安装/卸载/启停/更新一律 user scope +
// --yes（非 TTY 下免交互确认），与本应用「全局安装」的产品语义对齐。
//
// 多 CLI 扩展提示：本文件导出名（ListMarketplaces/InstallPlugin 等）为 claude 语义的
// 无前缀通用名——v1 单 CLI 下命名诚实。未来第二 CLI（codex 等）落地时，须把本文件
// 导出按 <CLI> 前缀规约重构（如 ClaudeListMarketplaces/ClaudeInstallPlugin），新增
// <cli>.go 对称实现，service.Operate 届时升级为 cli×action 二维分发。

// ClaudeMarketplaceEntry 对应 `claude plugin marketplace list --json` 的数组元素
// （实测 claude 2.1.228 字段；防御性最小集，CLI 增字段不破解析）。
type ClaudeMarketplaceEntry struct {
	Name            string `json:"name"`
	Source          string `json:"source"` // github | git | directory | file
	Repo            string `json:"repo"`   // source=github：owner/repo
	URL             string `json:"url"`    // source=git：git URL
	Path            string `json:"path"`   // source=directory/file：本地路径
	InstallLocation string `json:"installLocation"`
}

// SourceType 把 CLI 的 source 原始类型归一为前端展示口径，返回 (类型, 明细)：
// github → owner/repo；git → git URL；directory/file → 本地路径（引用原目录，不拷贝）。
func (e ClaudeMarketplaceEntry) SourceType() (string, string) {
	switch e.Source {
	case "github":
		return "github", e.Repo
	case "git":
		return "git", e.URL
	case "directory", "file":
		return "local", e.Path
	default:
		return e.Source, ""
	}
}

// ClaudeInstalledPlugin 对应 `claude plugin list --json` 的数组元素。
type ClaudeInstalledPlugin struct {
	ID          string `json:"id"` // name@marketplace
	Version     string `json:"version"`
	Scope       string `json:"scope"` // user | project | local
	Enabled     bool   `json:"enabled"`
	InstallPath string `json:"installPath"`
}

// ListMarketplaces 返回 claude 已注册的全部插件市场。
func ListMarketplaces() ([]ClaudeMarketplaceEntry, error) {
	out, err := run(timeoutLocal, "plugin", "marketplace", "list", "--json")
	if err != nil {
		return nil, err
	}
	var entries []ClaudeMarketplaceEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, fmt.Errorf("解析插件市场列表失败：%s", truncateRunes(err.Error(), 200))
	}
	return entries, nil
}

// AddMarketplace 注册插件市场。source 为 CLI 认可的三种形式之一：本地绝对路径 /
// GitHub owner/repo / git URL，透传不加工（本地路径为引用原目录，github/git 源由 CLI clone）。
func AddMarketplace(source string) error {
	_, err := run(timeoutClone, "plugin", "marketplace", "add", source)
	return err
}

// UpdateMarketplace 刷新插件市场（github/git 源重新拉取；本地目录源仅重校验清单）。
func UpdateMarketplace(name string) error {
	_, err := run(timeoutClone, "plugin", "marketplace", "update", name)
	return err
}

// RemoveMarketplace 注销插件市场。claude 语义：从最后一个 scope 移除时连带卸载该市场
// 已安装的全部插件——调用方（service/前端）须先经用户确认。
func RemoveMarketplace(name string) error {
	_, err := run(timeoutLocal, "plugin", "marketplace", "remove", name)
	return err
}

// ListInstalledPlugins 返回全部已安装插件（含各 scope 与 enabled 状态）。
func ListInstalledPlugins() ([]ClaudeInstalledPlugin, error) {
	out, err := run(timeoutLocal, "plugin", "list", "--json")
	if err != nil {
		return nil, err
	}
	var plugins []ClaudeInstalledPlugin
	if err := json.Unmarshal([]byte(out), &plugins); err != nil {
		return nil, fmt.Errorf("解析已安装插件列表失败：%s", truncateRunes(err.Error(), 200))
	}
	return plugins, nil
}

// InstallPlugin 安装插件（user scope 全局）。install 无确认交互（实测 2.1.228 无 --yes flag）。
func InstallPlugin(name, mkName string) error {
	_, err := run(timeoutClone, "plugin", "install", PluginRef(name, mkName), "--scope", "user")
	return err
}

// UninstallPlugin 卸载插件（不保留插件数据目录；--yes 跳过 prune 确认——非 TTY 下必需）。
func UninstallPlugin(name, mkName string) error {
	_, err := run(timeoutLocal, "plugin", "uninstall", PluginRef(name, mkName), "--scope", "user", "--yes")
	return err
}

// EnablePlugin 启用插件（user scope）。
func EnablePlugin(name, mkName string) error {
	_, err := run(timeoutLocal, "plugin", "enable", PluginRef(name, mkName), "--scope", "user")
	return err
}

// DisablePlugin 禁用插件（user scope，保留安装）。
func DisablePlugin(name, mkName string) error {
	_, err := run(timeoutLocal, "plugin", "disable", PluginRef(name, mkName), "--scope", "user")
	return err
}

// UpdatePlugin 更新已安装插件到市场最新版本（user scope，update 无确认交互）。
func UpdatePlugin(name, mkName string) error {
	_, err := run(timeoutClone, "plugin", "update", PluginRef(name, mkName), "--scope", "user")
	return err
}
