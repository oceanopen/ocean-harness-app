// Package agentcatalog ACP agent 目录（离线 pin 快照）。
//
// agent-catalog.json 由 scripts/prepare-agent-catalog.ts 从官方 ACP registry 生成
// （pnpm server:catalog:refresh，registry 原始快照同写入库），编译期 go:embed 内嵌
// sidecar，运行时不在线拉取。取值域 SSOT：launch_settings.agentCode 的合法取值 =
// 本目录 enabled 条目的 id（HTTP 面经 /api/agentCatalog/getList 投影给前端）。
package agentcatalog

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

//go:embed agent-catalog.json
var embedded embed.FS

// schemaVersion 当前 catalog 结构版本，与刷新脚本产出强校验（结构演进 = 递增 + 两端同步）。
const schemaVersion = 1

// Strategy spawn 策略（阶段 1 spawn 策略表）。
type Strategy string

const (
	// StrategyNpxAdapter 经 npx 拉起官方 adapter（claude 一期唯一启用；T1.3 起 spawn
	// 改指受管目录 vendored 入口，策略名不变）。
	StrategyNpxAdapter Strategy = "npx-adapter"
	// StrategyNativeACP 直连 agent 自带 ACP 的 CLI（codex / opencode / pi，随 P4 扩展启用）。
	StrategyNativeACP Strategy = "native-acp"
	// StrategyInProcessGo 预留枚举：Go 进程内实现 ACP agent 角色，SpawnConfig 翻译不支持。
	StrategyInProcessGo Strategy = "in-process-go"
)

// Source catalog 溯源三元组（registry 快照来源与抓取时点）。
type Source struct {
	URL             string `json:"url"`
	RegistryVersion string `json:"registryVersion"`
	FetchedAt       string `json:"fetchedAt"`
}

// Entry 单个 agent 条目。Command+Args 即 spawn argv 原始形态（npx 条目已含 pin 版本），
// 翻译为 acp.SpawnConfig 见 spawn.go。
type Entry struct {
	ID             string            `json:"id"`
	Code           string            `json:"code"` // agentCode（launch_settings.agentCode 取值）
	Label          string            `json:"label"`
	Version        string            `json:"version"`
	Description    string            `json:"description"`
	Strategy       Strategy          `json:"strategy"`
	Command        string            `json:"command"`
	Args           []string          `json:"args"`
	Env            map[string]string `json:"env"`
	Enabled        bool              `json:"enabled"`
	NodeMinVersion string            `json:"nodeMinVersion"` // node 主版本下限（T1.4 doctor 消费；空 = 不校验）
}

// Catalog agent-catalog.json 的结构化形态。
type Catalog struct {
	SchemaVersion int     `json:"schemaVersion"`
	Source        Source  `json:"source"`
	Agents        []Entry `json:"agents"`
}

var loadCatalog = sync.OnceValues(func() (*Catalog, error) {
	data, err := embedded.ReadFile("agent-catalog.json")
	if err != nil {
		return nil, fmt.Errorf("读取内嵌 agent catalog: %w", err)
	}
	return parse(data)
})

// Load 返回内嵌 catalog（进程级单例）。失败仅可能源于二进制产物损坏，
// catalog_test 对内嵌产物有常驻断言兜底。
func Load() (*Catalog, error) { return loadCatalog() }

// GetAgentCatalogInfoByCode 按 agentCode 取 catalog 记录（匹配条目 code）。Load 失败或
// 无此条目时返回零值 + false（产物损坏场景由包测试与消费端显式处置）。
func GetAgentCatalogInfoByCode(agentCode string) (Entry, bool) {
	cat, err := Load()
	if err != nil {
		return Entry{}, false
	}
	for _, entry := range cat.Agents {
		if entry.Code == agentCode {
			return entry, true
		}
	}
	return Entry{}, false
}

// Enabled 返回 enabled 条目，保持 JSON 顺序（UI 下拉顺序即 catalog 顺序）。
func Enabled() []Entry {
	cat, err := Load()
	if err != nil {
		return nil
	}
	var entries []Entry
	for _, entry := range cat.Agents {
		if entry.Enabled {
			entries = append(entries, entry)
		}
	}
	return entries
}

// parse 解析并校验 catalog：结构版本、条目非空、id/code 唯一非空、strategy 枚举合法、
// enabled 条目 command 非空。宁可失败不静默缺刊（Gold-Band 同款语义）。
func parse(data []byte) (*Catalog, error) {
	var cat Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return nil, fmt.Errorf("解析 agent catalog: %w", err)
	}
	if cat.SchemaVersion != schemaVersion {
		return nil, fmt.Errorf("agent catalog schemaVersion = %d, want %d", cat.SchemaVersion, schemaVersion)
	}
	if len(cat.Agents) == 0 {
		return nil, errors.New("agent catalog 无条目")
	}
	seen := make(map[string]struct{}, len(cat.Agents))
	seenCode := make(map[string]struct{}, len(cat.Agents))
	for _, entry := range cat.Agents {
		if strings.TrimSpace(entry.ID) == "" {
			return nil, errors.New("agent catalog 存在空 id 条目")
		}
		if _, dup := seen[entry.ID]; dup {
			return nil, fmt.Errorf("agent catalog id 重复: %q", entry.ID)
		}
		seen[entry.ID] = struct{}{}
		if strings.TrimSpace(entry.Code) == "" {
			return nil, fmt.Errorf("agent %q code 为空", entry.ID)
		}
		if _, dup := seenCode[entry.Code]; dup {
			return nil, fmt.Errorf("agent catalog code 重复: %q", entry.Code)
		}
		seenCode[entry.Code] = struct{}{}
		switch entry.Strategy {
		case StrategyNpxAdapter, StrategyNativeACP, StrategyInProcessGo:
		default:
			return nil, fmt.Errorf("agent %q strategy %q 非法", entry.ID, entry.Strategy)
		}
		if entry.Enabled && strings.TrimSpace(entry.Command) == "" {
			return nil, fmt.Errorf("agent %q enabled 但 command 为空", entry.ID)
		}
	}
	return &cat, nil
}
