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
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"ocean-harness/server/internal/agentcatalog"
)

//go:embed browser-mcp.json
var pinJSON []byte

// enginePin 引擎 pin 投影（browser-mcp.json）：版本 SSOT 在 pnpm-workspace.yaml
// catalog 段（与 acp 包同款机制），构建期由 scripts/vendor-browser-engine.ts 防漂移
// 断言对齐两者；Go 不读 yaml，运行期构造 Entry 消费此投影，只读。
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

// engineConfigJSON 引擎启动配置（落盘后经 --config 交引擎，playwright-mcp config 格式）：
// ignoreDefaultArgs 去掉 playwright 默认注入的 --disable-blink-features=AutomationControlled
// ——品牌 Chrome 坏标记警示条（bad_flags_prompt 的 kBadFlags 含 kDisableBlinkFeatures）
// 在有头模式恒弹黄条；该标记在 CfT 155 实测已不影响 navigator.webdriver（缺省即 true），
// 去掉无反检测损失。channel="chromium" 为 playwright 官方 bundled-chromium channel：
// registry 选可执行文件时 channel 非空即绕开 headless-shell 分支——内置分发经 --no-shell
// 只装完整版（无头也由它以 --headless=new 承担），不指定 channel 的默认选择（headless →
// chromium-headless-shell）会因 shell 缺失 spawn 失败；PLAYWRIGHT_BROWSERS_PATH 定位不变。
// launchOptions.args 注入 --remote-debugging-port=0（D9 CDP 观察通道常态化）：随机端口
// 随浏览器启动落盘 DevToolsActivePort 供 live viewer 读取；T4.1 S1 实测与 playwright
// 恒需的 --remote-debugging-pipe 无互斥（launchOptions.args 原样追加给 Chromium）。
// CLI flags（--user-data-dir 等）优先级高于 config，互不冲突。
const engineConfigJSON = `{"browser":{"launchOptions":{"ignoreDefaultArgs":["--disable-blink-features=AutomationControlled"],"channel":"chromium","args":["--remote-debugging-port=0"]}}}`

// EnsureEngineConfig 幂等落盘引擎启动配置，返回 --config 消费的文件路径。
func EnsureEngineConfig() (string, error) {
	path := filepath.Join(Root(), "engine.json")
	if data, err := os.ReadFile(path); err == nil && string(data) == engineConfigJSON {
		return path, nil
	}
	if err := os.WriteFile(path, []byte(engineConfigJSON), 0o644); err != nil {
		return "", fmt.Errorf("写入引擎启动配置: %w", err)
	}
	return path, nil
}

// EngineSpawnArgs 组装引擎 argv 尾参（node 入口路径之后）。
//
//	（无 --browser）            playwright 内置 Chromium build（T3.5/D2 修订：嵌入分发，
//	                            PLAYWRIGHT_BROWSERS_PATH 定位；系统 chrome channel 退役）
//	--user-data-dir             per-profile 独立数据目录（登录态活 SSOT，D3）
//	--caps                      能力面常量
//	--output-dir                下载/产物落点（缺省 cwd/.playwright-mcp——spike 实锤必显式传）
//	--idle-timeout              兜底层 idle（毫秒，D4）
//	--no-webmcp                 工具面静态，免 tools/list 动态变更处理
//	--file-paths absolute       路径回传口径（工具结果回传绝对路径）
//	--config                    引擎启动配置（engine.json，去 AutomationControlled 默认标记）
//	--headless                  per-profile 无头偏好为真时追加（D10）
func EngineSpawnArgs(profileDir, downloadDir, configPath string, headless bool) []string {
	args := []string{
		"--user-data-dir", profileDir,
		"--caps", engineCaps,
		"--output-dir", downloadDir,
		"--idle-timeout", strconv.Itoa(engineIdleTimeoutMS),
		"--no-webmcp",
		"--file-paths", "absolute",
		"--config", configPath,
	}
	if headless {
		args = append(args, "--headless")
	}
	return args
}
