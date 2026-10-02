// Package clibin login shell 环境解析。GUI 拉起的 sidecar PATH 常缺用户 shell 的
// nvm/volta 等目录（与 Tauri 侧终端链路同源问题），本包经 login+interactive shell
// 跑一次 `/usr/bin/env` 拿到 login PATH，供 node 等运行时二进制的兜底解析与 claude
// 子进程 env 注入（bot turnEnv / ACP ClaudeEnvOverrides）。
//
// claude 二进制不经此包解析：P5 定稿终态为 vendored 自带 claude 全链路 SSOT
// （agentcatalog.VendoredClaudeBin），终端手动路径由用户 shell 自解析。
package clibin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// login shell 探测的进程级成功缓存（失败不缓存，下次调用重探）。login+interactive
// shell 带全量 rc 启动，毫秒到百毫秒级，不宜每次调用重跑。
var (
	loginProbeMu     sync.Mutex
	loginProbeStdout string
	loginProbeDone   bool
)

// loginProbeOutput 执行 login shell 环境探测并缓存成功输出；失败原样返回错误、不缓存。
// stdin 关死（Cmd.Stdin 默认 nil = /dev/null）：rc 脚本若读 stdin 会挂起探测进程。
func loginProbeOutput() (string, error) {
	loginProbeMu.Lock()
	defer loginProbeMu.Unlock()
	if loginProbeDone {
		return loginProbeStdout, nil
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	out, err := exec.Command(shell, "-l", "-i", "-c", "/usr/bin/env").Output()
	if err != nil {
		return "", err
	}
	loginProbeStdout, loginProbeDone = string(out), true
	return loginProbeStdout, nil
}

// LoginPath 返回用户 login shell 的 PATH（$SHELL -l -i -c 探测，进程级成功缓存）。
// GUI 拉起的 sidecar PATH 缺 nvm/volta 等目录时，供 node 等运行时二进制的兜底解析
// （agentcatalog vendored spawn 消费）与 claude 子进程 env 注入。
func LoginPath() (string, error) {
	stdout, err := loginProbeOutput()
	if err != nil {
		return "", fmt.Errorf("login shell 探测: %w", err)
	}
	if path := lastEnvPath(stdout); path != "" {
		return path, nil
	}
	return "", errors.New("login shell 探测输出无 PATH")
}

// lastEnvPath 取输出中最后一条 `PATH=` 行的值（env 输出在脚本末尾，覆盖 rc 噪声可能
// 打印的 PATH= 行）。
func lastEnvPath(stdout string) string {
	path := ""
	for _, line := range strings.Split(stdout, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "PATH=") {
			path = strings.TrimPrefix(line, "PATH=")
		}
	}
	return path
}
