package acp

import (
	"errors"
	"maps"

	"ocean-harness/server/internal/clibin"
)

/*
 * claudeExecutableEnv adapter→SDK 的 claude 可执行文件指定环境变量：adapter 的 claudeCliPath() 以本 env 为最高优先级，值直传 SDK pathToClaudeCodeExecutable；缺省时
 * adapter 自行回落解析（vendored 形态即 SDK optionalDependencies 自带的原生二进制）。显式注入 vendored 定位结果而非依赖 SDK 隐式回落，保持「探测通过 = 生产链路能拉起」零差异
 * （P5 定稿终态：vendored 自带 claude 全链路 SSOT）。
 */
const claudeExecutableEnv = "CLAUDE_CODE_EXECUTABLE"

// DisableAutoupdateEnv vendored claude 版本构建期锁死（零 registry 依赖）：全链路注入禁 CLI 自更新，防更新器借 vendored node 跑 npm 写穿应用资源目录。
const DisableAutoupdateEnv = "DISABLE_AUTOUPDATE"

/*
 * ClaudeEnvOverrides 在 base 之上合并 claude 一致性注入并返回新 map（不改 base），消费方拼装进 SpawnConfig.Env、client 本体不感知 claude（D5）。
 * CLAUDE_CODE_EXECUTABLE = vendored 自带二进制绝对路径（终端直启 / ACP / bot / marketplace 同源零漂移）；PATH = login shell PATH 全量覆盖（best-effort，探测失败保留原值）；DISABLE_AUTOUPDATE 禁 CLI 自更新。
 */
func ClaudeEnvOverrides(base map[string]string, claudeBin string) (map[string]string, error) {
	if claudeBin == "" {
		return nil, errors.New("claude 二进制路径为空（应由消费方先经 agentcatalog.VendoredClaudeBin 解析）")
	}
	merged := maps.Clone(base)
	if merged == nil {
		merged = make(map[string]string, 2)
	}
	merged[claudeExecutableEnv] = claudeBin
	merged[DisableAutoupdateEnv] = "1"
	if loginPath, err := clibin.LoginPath(); err == nil && loginPath != "" {
		merged["PATH"] = loginPath
	}
	return merged, nil
}
