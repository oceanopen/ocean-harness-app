// Package clibin CLI Agent 可执行文件解析——bot / acp / marketplace / agentcatalog
// 共用的中立 SSOT，不依赖任何业务域。当前仅 claude；扩展新 CLI 时按同款三级链新增
// 解析函数。LoginPath 另行暴露 login shell 的 PATH，供 node 等运行时二进制的兜底解析。
package clibin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// GUI 拉起的 sidecar 缺用户 shell 的 nvm/volta 等 PATH 目录（与 Tauri 侧
// app/src/pty/cli_bin.rs 同源问题），故三级探测链：
//  1. LookPath（微秒级；命中说明 GUI PATH 本就有 claude，无需注入）
//  2. 常见安装位置兜底：brew（Intel/Apple Silicon）+ ~/.claude/local（官方本地安装脚本布局）
//  3. login shell 哨兵探测（镜像 cli_bin.rs：$SHELL -l -i -c 输出 `command -v claude` 与
//     `/usr/bin/env`，哨兵隔离 rc 噪声）——同时拿 login PATH 注入子进程 env，防 npm/nvm
//     安装的 node shebang CLI 因找不到 node 起不来（brew 自包含二进制注入无害）
//
// 缓存：只缓存成功结果（bin + loginPath 对）；失败不缓存，下次调用重探——用户装上
// CLI 后无需重启 sidecar 即生效。token 固定 "claude"，探测脚本无外部注入面。
const claudeToken = "claude"

var claudePathFallbacks = []string{"/usr/local/bin/claude", "/opt/homebrew/bin/claude"}

// ClaudeBin 解析结果。LoginPath 仅第 3 级探测命中时非空，语义是 spawn 时注入子进程
// env 的 login PATH。
type ClaudeBin struct {
	Bin       string // claude 绝对路径
	LoginPath string
}

var (
	claudeBinMu     sync.Mutex
	claudeBinCached *ClaudeBin
)

// Resolve 解析 claude 可执行文件（带成功缓存）。失败返回中文错误并列出已试位置。
func Resolve() (ClaudeBin, error) {
	claudeBinMu.Lock()
	defer claudeBinMu.Unlock()
	if claudeBinCached != nil {
		return *claudeBinCached, nil
	}

	// 1) GUI PATH 直查。
	if p, err := exec.LookPath(claudeToken); err == nil {
		return cacheClaudeBin(ClaudeBin{Bin: p}), nil
	}

	// 2) 常见安装位置兜底。
	probes := append([]string{}, claudePathFallbacks...)
	if home, err := os.UserHomeDir(); err == nil {
		probes = append(probes, filepath.Join(home, ".claude", "local", claudeToken))
	}
	for _, p := range probes {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return cacheClaudeBin(ClaudeBin{Bin: p}), nil
		}
	}

	// 3) login shell 哨兵探测（覆盖 nvm/volta 等只在用户 shell PATH 里的安装）。
	if probed := probeClaudeInLoginShell(); probed != nil {
		return cacheClaudeBin(*probed), nil
	}

	return ClaudeBin{}, errors.New(
		"未找到 claude 命令：请先安装 Claude Code CLI（已尝试 PATH、brew 安装位置、~/.claude/local 与 login shell 探测）")
}

func cacheClaudeBin(b ClaudeBin) ClaudeBin {
	claudeBinCached = &b
	return b
}

// 哨兵标记：隔离 `command -v` 输出与 rc 文件噪声（cli_bin.rs 同款——rc 在 -c 脚本执行前跑完，
// 输出在时间上不可能插入哨兵之间；裸取「首个 / 开头行」会把 rc 的绝对路径报错行误判为 CLI）。
const (
	probeSentinelOpen  = "OCEAN_CLIBIN_PROBE_OPEN"
	probeSentinelClose = "OCEAN_CLIBIN_PROBE_CLOSE"
)

// login shell 探测的进程级成功缓存（对齐 claudeBin 缓存口径：失败不缓存，下次调用
// 重探）。claude 探测与 LoginPath 共享同一次子进程输出——login+interactive shell 带全量
// rc 启动，毫秒到百毫秒级，不宜每次调用重跑。
var (
	loginProbeMu     sync.Mutex
	loginProbeStdout string
	loginProbeDone   bool
)

// loginProbeOutput 执行 login shell 哨兵探测并缓存成功输出；失败原样返回错误、不缓存。
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
	script := fmt.Sprintf("echo %s; command -v %s; echo %s; /usr/bin/env",
		probeSentinelOpen, claudeToken, probeSentinelClose)
	// Cmd.Stdin 默认 nil = /dev/null（stdin 关死，防 rc 脚本读 stdin 挂起）。
	out, err := exec.Command(shell, "-l", "-i", "-c", script).Output()
	if err != nil {
		return "", err
	}
	loginProbeStdout, loginProbeDone = string(out), true
	return loginProbeStdout, nil
}

// probeClaudeInLoginShell 从缓存的探测输出解析 claude 绝对路径与 login PATH；失败返回
// nil（回落到上层错误文案）。
func probeClaudeInLoginShell() *ClaudeBin {
	stdout, err := loginProbeOutput()
	if err != nil {
		return nil
	}
	return parseClaudeProbe(stdout)
}

// LoginPath 返回用户 login shell 的 PATH（$SHELL -l -i -c 探测，进程级成功缓存）。
// GUI 拉起的 sidecar PATH 缺 nvm/volta 等目录时，供 node 等运行时二进制的兜底解析
// （agentcatalog vendored spawn 消费）。
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

// parseClaudeProbe 解析探测输出：哨兵之间首个 `/` 开头行 = CLI 绝对路径（`command -v` 对
// builtin/alias 返回名字本身而非路径，天然判失败）；PATH 取 lastEnvPath。任一缺失 → nil。
func parseClaudeProbe(stdout string) *ClaudeBin {
	var bin string
	inBlock := false
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == probeSentinelOpen:
			inBlock = true
		case line == probeSentinelClose:
			inBlock = false
		case inBlock && bin == "" && strings.HasPrefix(line, "/"):
			bin = line
		}
	}
	path := lastEnvPath(stdout)
	if bin == "" || path == "" {
		return nil
	}
	return &ClaudeBin{Bin: bin, LoginPath: path}
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
