package marketplace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// manifest.go：marketplace 清单（.claude-plugin/marketplace.json）扫描与 CLI 支持探测。
// 清单 schema 依据 Claude Code 官方 marketplace-reference（name/owner/plugins[]，可选
// metadata.description/metadata.version 备选位置）；解析取前端展示所需最小字段集。

// manifestRelPath 是市场清单相对市场根目录的固定位置（官方约定）。
const manifestRelPath = ".claude-plugin/marketplace.json"

// MarketplaceManifest 是市场清单的解析结果。
type MarketplaceManifest struct {
	Name        string
	Description string
	Version     string
	Plugins     []ManifestPlugin
}

// ManifestPlugin 是市场内一个插件条目。
type ManifestPlugin struct {
	Name           string
	Source         string // 原始 source：相对路径字符串；对象形态为紧凑 JSON
	SourceExternal bool   // source 为外部引用（github/npm/archive 等），本地无目录可扫描
	Dir            string // 相对路径 source 解析出的插件目录绝对路径；外部引用为空
	Description    string
	Version        string // plugin.json version 优先，回落市场条目 version
	Components     PluginComponents
}

// PluginComponents 是插件目录按标准布局统计的组件构成（目录/文件缺失计 0）。
type PluginComponents struct {
	Commands   int // commands/*.md（扁平，非递归）
	Skills     int // skills/<name>/SKILL.md 子目录数；插件根单 SKILL.md 布局计 1
	Agents     int // agents/*.md
	Hooks      int // hooks/hooks.json 存在计 1
	McpServers int // .mcp.json 存在计 1
}

// LoadManifest 读取并解析 root 下的市场清单，并逐插件解析目录与组件统计。
// root 为 claude 侧 installLocation（本地目录源=原路径；github/git 源=clone 目录），
// 两条路径下清单均在本地的 <root>/.claude-plugin/marketplace.json。
func LoadManifest(root string) (*MarketplaceManifest, error) {
	raw, err := os.ReadFile(filepath.Join(root, manifestRelPath))
	if err != nil {
		return nil, fmt.Errorf("读取市场清单失败（%s）：%s", manifestRelPath, truncateRunes(err.Error(), 160))
	}
	var doc struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		Metadata    struct {
			Description string `json:"description"`
			Version     string `json:"version"`
		} `json:"metadata"`
		Plugins []struct {
			Name        string          `json:"name"`
			Source      json.RawMessage `json:"source"`
			Description string          `json:"description"`
			Version     string          `json:"version"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("市场清单 JSON 解析失败：%s", truncateRunes(err.Error(), 200))
	}
	mf := &MarketplaceManifest{
		Name:        doc.Name,
		Description: firstNonEmpty(doc.Description, doc.Metadata.Description),
		Version:     firstNonEmpty(doc.Version, doc.Metadata.Version),
		Plugins:     make([]ManifestPlugin, 0, len(doc.Plugins)),
	}
	for _, p := range doc.Plugins {
		mp := ManifestPlugin{
			Name:        p.Name,
			Description: p.Description,
			Version:     p.Version,
		}
		mp.Source, mp.SourceExternal = classifySource(p.Source)
		if !mp.SourceExternal && mp.Source != "" && !strings.Contains(mp.Source, "..") {
			mp.Dir = filepath.Join(root, mp.Source)
			mp.Components = countComponents(mp.Dir)
			if mp.Version == "" {
				mp.Version = pluginJSONVersion(mp.Dir)
			}
		}
		mf.Plugins = append(mf.Plugins, mp)
	}
	return mf, nil
}

// DetectSupportedClis 按清单目录约定探测市场支持的 CLI 开发工具。
// v1 约定：存在 .claude-plugin/marketplace.json 即支持 claude。未来其他 CLI（codex/gemini
// 等）在同仓库落自己的清单目录后，在此追加对应探测分支即自动出现在 supportedClis。
func DetectSupportedClis(root string) []string {
	var clis []string
	if HasManifest(root) {
		clis = append(clis, "claude")
	}
	return clis
}

// HasManifest 判断 dir 是否持有市场清单（即是否为合法市场目录形态）。
// 导出给 service 层做添加预检等场景复用——清单路径约定以本包为单源，勿在包外拼接。
func HasManifest(dir string) bool {
	return fileExists(filepath.Join(dir, manifestRelPath))
}

// PluginRef 拼 claude 的复合插件 id：name@marketplace（跨市场同名插件以此消歧）。
// 导出给 service 层做安装状态 join 复用，id 形态变化时只改此处。
func PluginRef(name, mkName string) string {
	return name + "@" + mkName
}

// classifySource 归一化市场条目的 source 字段：字符串形态 = 市场内相对路径（./ 前缀，
// 解析基准为市场根）；对象形态 = 外部引用（github/git/git-subdir/npm/archive/command），
// 以紧凑 JSON 返回供展示。空值返回 ("", false) 由调用方跳过目录解析。
func classifySource(raw json.RawMessage) (string, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "", false
	}
	if trimmed[0] == '"' {
		var rel string
		if err := json.Unmarshal(raw, &rel); err != nil {
			return "", true
		}
		return rel, false
	}
	// 对象形态：紧凑 JSON 原样展示（无需解析内部字段，本地也无目录可统计）。
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return trimmed, true
	}
	return buf.String(), true
}

// countComponents 按官方标准布局统计插件目录组件构成；目录不存在/不可读时全部为 0（不报错，
// 组件统计是展示性信息，不阻塞列表）。
func countComponents(dir string) PluginComponents {
	var c PluginComponents
	c.Commands = countMdFiles(filepath.Join(dir, "commands"))
	c.Agents = countMdFiles(filepath.Join(dir, "agents"))
	c.Skills = countSkills(filepath.Join(dir, "skills"))
	// 官方布局：插件根直接放 SKILL.md 且无 skills/ 目录时整个插件视为单 skill。
	if c.Skills == 0 && fileExists(filepath.Join(dir, "SKILL.md")) {
		c.Skills = 1
	}
	if fileExists(filepath.Join(dir, "hooks", "hooks.json")) {
		c.Hooks = 1
	}
	if fileExists(filepath.Join(dir, ".mcp.json")) {
		c.McpServers = 1
	}
	return c
}

// countMdFiles 统计目录下一层 *.md 文件数（commands/agents 为扁平 Markdown 约定）。
func countMdFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			n++
		}
	}
	return n
}

// countSkills 统计 skills/ 下含 SKILL.md 的子目录数。
func countSkills(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && fileExists(filepath.Join(dir, e.Name(), "SKILL.md")) {
			n++
		}
	}
	return n
}

// pluginJSONVersion 读取插件目录 .claude-plugin/plugin.json 的 version；失败返回 ""。
func pluginJSONVersion(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		return ""
	}
	var doc struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	return doc.Version
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// fileExists 判断路径存在（文件或目录）。
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
