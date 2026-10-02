package acp

import (
	"errors"
	"maps"

	"ocean-harness/server/internal/clibin"
)

// claudeExecutableEnv adapter→SDK 的 claude 可执行文件指定环境变量：adapter 的
// claudeCliPath() 以本 env 为最高优先级，值直传 SDK pathToClaudeCodeExecutable；缺省时
// adapter 自行回落解析（vendored 形态即 SDK optionalDependencies 自带的原生二进制）。
// 显式注入 vendored 定位结果而非依赖 SDK 隐式回落，保持「探测通过 = 生产链路能拉起」
// 零差异（P5 定稿终态：vendored 自带 claude 全链路 SSOT）。
const claudeExecutableEnv = "CLAUDE_CODE_EXECUTABLE"

// ClaudeEnvOverrides 在 base 之上合并 claude 路径一致性注入（T1.3/P5 定稿终态），返回
// 新 map 不改 base；由消费方拼装进 SpawnConfig.Env，client 本体不感知 claude（D5）。
//   - CLAUDE_CODE_EXECUTABLE = claudeBin（消费方先经 agentcatalog.VendoredClaudeBin
//     解析的 vendored 自带二进制绝对路径）：终端直启 / ACP / bot / marketplace 消费
//     同一份 claude，零版本漂移。
//   - PATH = login shell PATH（探测成功时全量覆盖，best-effort）：GUI 拉起的 sidecar
//     常缺 nvm 等路径，claude 派生的工具子进程同享一致环境（对齐 bot driver_claude
//     turnEnv 惯例）；探测失败保留原 PATH。
func ClaudeEnvOverrides(base map[string]string, claudeBin string) (map[string]string, error) {
	if claudeBin == "" {
		return nil, errors.New("claude 二进制路径为空（应由消费方先经 agentcatalog.VendoredClaudeBin 解析）")
	}
	merged := maps.Clone(base)
	if merged == nil {
		merged = make(map[string]string, 2)
	}
	merged[claudeExecutableEnv] = claudeBin
	if loginPath, err := clibin.LoginPath(); err == nil && loginPath != "" {
		merged["PATH"] = loginPath
	}
	return merged, nil
}
