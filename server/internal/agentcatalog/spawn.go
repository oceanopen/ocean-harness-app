package agentcatalog

import (
	"fmt"
	"maps"

	"ocean-harness/server/internal/acp"
)

// SpawnConfig 将条目翻译为 acp.SpawnConfig（T1.5 会话域 / T1.4 doctor 的拉起入口）。
// Cwd 由调用方传（workspace 目录，同为 session/new 缺省目录候选）；vendored 入口改写走
// VendoredSpawnConfig（vendored.go），CLAUDE_CODE_EXECUTABLE / login PATH 注入由调用方
// 在返回值的 Env 上追加，catalog 层不感知 claude 专属逻辑（D5：单一通用 adapter，agent 差异止于条目数据）。
func (e Entry) SpawnConfig(cwd string) (acp.SpawnConfig, error) {
	switch e.Strategy {
	case StrategyNpxAdapter, StrategyNativeACP:
		// 两策略当前同为 argv 直拼：差异在 T1.3 vendoring 只改写 npx-adapter 条目。
		return acp.SpawnConfig{
			Command: append([]string{e.Command}, e.Args...),
			Env:     maps.Clone(e.Env),
			Cwd:     cwd,
		}, nil
	case StrategyInProcessGo:
		return acp.SpawnConfig{}, fmt.Errorf("agent %q 策略 %s 为预留枚举，无进程外拉起形态", e.ID, e.Strategy)
	default:
		return acp.SpawnConfig{}, fmt.Errorf("agent %q strategy %q 非法", e.ID, e.Strategy)
	}
}
