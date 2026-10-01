package acp

import (
	"fmt"
	"maps"

	"ocean-harness/server/internal/clibin"
)

// claudeExecutableEnv adapter→SDK 的 claude 可执行文件指定环境变量：0.84.0 adapter 的
// claudeCliPath() 以本 env 为最高优先级，值直传 SDK pathToClaudeCodeExecutable；缺省时
// adapter 回落 SDK optionalDependencies 自带的原生二进制——与终端模式的 claude 是两个
// 二进制（版本漂移），本注入即 T1.3 的一致性闸门。
const claudeExecutableEnv = "CLAUDE_CODE_EXECUTABLE"

// ClaudeEnvOverrides 在 base 之上合并 claude 路径一致性注入（T1.3/P5），返回新 map 不改
// base；由消费方拼装进 SpawnConfig.Env，client 本体不感知 claude（D5）。
//   - CLAUDE_CODE_EXECUTABLE = clibin 解析的 claude：ACP 与终端模式共享同一份 CLI，
//     兜底 = login PATH（adapter 探测链若变，PATH 命中同一个 claude）。
//   - PATH = login shell PATH（有探测结果时全量覆盖）：GUI 拉起的 sidecar 常缺 nvm 等
//     路径，claude 派生的工具子进程同享一致环境（对齐 bot driver_claude turnEnv 惯例）。
//
// claude 未安装即返回 clibin 探测失败错误（ACP 拉起期报错，不阻断 server 启动）。
func ClaudeEnvOverrides(base map[string]string) (map[string]string, error) {
	resolved, err := clibin.Resolve()
	if err != nil {
		return nil, fmt.Errorf("ACP claude 路径解析: %w", err)
	}
	merged := maps.Clone(base)
	if merged == nil {
		merged = make(map[string]string, 2)
	}
	merged[claudeExecutableEnv] = resolved.Bin
	if resolved.LoginPath != "" {
		merged["PATH"] = resolved.LoginPath
	}
	return merged, nil
}
