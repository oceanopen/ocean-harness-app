// Package browser 浏览器自动化引擎域：vendored @playwright/mcp 引擎子进程的版本 pin、
// 目录布局、拉起与 go-sdk stdio 桥接（T1.2）。会话管理与实时投影见 manager（T1.3）。
//
// 引擎消费面：agentcatalog.EnsureVendored 复用既有 vendoring 机制（Entry 包外字面量
// 构造），spawn argv = node <vendored 入口> <引擎 flags>；进程纪律（独立进程组 + 整组
// 强杀）复用 acp 包导出面。
package browser

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"ocean-harness/server/internal/agentcatalog"
)

//go:embed browser-mcp.json
var pinJSON []byte

// enginePin 版本 pin SSOT（browser-mcp.json），构建期锁定、运行时只读。
type enginePin struct {
	Package string `json:"package"`
	Version string `json:"version"`
}

var loadPin = sync.OnceValues(func() (enginePin, error) {
	var p enginePin
	if err := json.Unmarshal(pinJSON, &p); err != nil {
		return enginePin{}, fmt.Errorf("解析 browser-mcp.json: %w", err)
	}
	if p.Package == "" || p.Version == "" {
		return enginePin{}, errors.New("browser-mcp.json 的 package/version 不可为空")
	}
	return p, nil
})

// 引擎 spawn flags 常量（flag 拼写与语义为 T1.1 外部核实采信结论）。
const (
	// engineCaps 引擎能力面（D5 精选转发 + 6 原生的前提 cap；合法全集
	// config,network,pdf,storage,testing,vision,devtools——不开的 cap 其工具不进
	// tools/list，逃生舱也摸不到）。
	engineCaps = "storage,network,pdf"

	// engineIdleTimeoutMS 兜底层 idle 释放（毫秒，D4 双层的引擎侧）：manager 主层
	// 10 分钟，兜底层宽一档 15 分钟。到期只关浏览器窗口——引擎 node 进程存活、下次
	// 调用自愈重启浏览器（登录态在 profile 目录续存），防 manager 异常时 Chrome 常驻
	// 不释放。有头默认 never，故必须显式传值。
	engineIdleTimeoutMS = 900000
)

// EngineEntry 构造引擎的 agentcatalog 条目（包外字面量复用既有 vendoring 机制：
// EnsureVendored 消费 Strategy/ID/Version，VendoredSpawnConfig 经 Args 还原包名并解析
// bin 入口）。@playwright/mcp 是薄壳包——实现在依赖闭包的 playwright-core 里，锁版由
// pnpm 依赖闭包自动携带（playwright/playwright-core 精确 pin）。
func EngineEntry() (agentcatalog.Entry, error) {
	p, err := loadPin()
	if err != nil {
		return agentcatalog.Entry{}, err
	}
	return agentcatalog.Entry{
		ID:       "playwright-mcp",
		Label:    "playwright-mcp",
		Version:  p.Version,
		Strategy: agentcatalog.StrategyNpxAdapter,
		Command:  "npx",
		Args:     []string{"-y", p.Package + "@" + p.Version},
	}, nil
}

// EngineVersion 返回 pin 的引擎版本（展示与错误归因用）。
func EngineVersion() (string, error) {
	p, err := loadPin()
	return p.Version, err
}

// EngineSpawnArgs 组装引擎 argv 尾参（node 入口路径之后）。
//
//	--browser chrome            系统浏览器 channel 探测交还 playwright（D2）
//	--user-data-dir             per-profile 独立数据目录（登录态活 SSOT，D3）
//	--caps                      能力面常量
//	--output-dir                下载/产物落点（缺省 cwd/.playwright-mcp——spike 实锤必显式传）
//	--idle-timeout              兜底层 idle（毫秒，D4）
//	--no-webmcp                 工具面静态，免 tools/list 动态变更处理
//	--file-paths absolute       路径回传口径（工具结果回传绝对路径）
//	--headless                  per-profile 无头偏好为真时追加（D10）
func EngineSpawnArgs(profileDir, downloadDir string, headless bool) []string {
	args := []string{
		"--browser", "chrome",
		"--user-data-dir", profileDir,
		"--caps", engineCaps,
		"--output-dir", downloadDir,
		"--idle-timeout", strconv.Itoa(engineIdleTimeoutMS),
		"--no-webmcp",
		"--file-paths", "absolute",
	}
	if headless {
		args = append(args, "--headless")
	}
	return args
}
