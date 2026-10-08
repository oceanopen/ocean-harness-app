// Package marketplace 插件市场域基础能力：封装 claude plugin CLI 调用（os/exec）与
// marketplace 清单扫描。全部为纯函数；插件安装状态与市场注册表的 SSOT 恒为 claude 侧
// （~/.claude/plugins），本包只做投影与透传，不落任何本地状态。
// 未来接入其他 CLI 开发工具（codex/gemini 等）时：按同包新增 <cli>.go 命令构造文件 +
// 在 manifest.go 的 DetectSupportedClis 追加该 CLI 的清单目录约定即可。
package marketplace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"ocean-harness/server/internal/agentcatalog"
	"ocean-harness/server/internal/clibin"
	"ocean-harness/server/internal/dal/enums"
)

// CLI 超时口径：add/update 涉及 git clone（受网络影响）放宽；其余为本地操作。
const (
	timeoutClone = 120 * time.Second
	timeoutLocal = 60 * time.Second
)

// ResolveClaudeBin 解析 claude 可执行文件路径（P5 定稿终态：vendored 自带 claude，
// 全链路 SSOT；vendoring 两目录由 main 启动期注入 agentcatalog，成功进程级缓存）。
func ResolveClaudeBin() (string, error) {
	return agentcatalog.ResolveVendoredClaudeBin(string(enums.AGENT_CODE_CLAUDE_ACP))
}

// run 执行 `claude <args...>`：context 超时兜底、分离捕获 stdout/stderr。
// 非零退出返回中文错误（附 stderr 摘要）；成功返回原始 stdout（JSON 交由调用方解析）。
// 二进制路径经 agentcatalog 进程级成功缓存：首次解析成功后固定，vendored 版本切换
// 需重启 sidecar 生效（失败不缓存，安装修复后无需重启）。
func run(timeout time.Duration, args ...string) (string, error) {
	bin, err := ResolveClaudeBin()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	// login PATH 注入（best-effort，对齐 bot driver_claude turnEnv / ACP ClaudeEnvOverrides
	// 惯例，本函数是 claude 调用方三链路中最后补齐的一个）：GUI 拉起的 sidecar PATH 常缺
	// nvm/homebrew 等目录，marketplace add/update 派生的 git 等工具子进程同享完整环境；
	// 探测失败保留原环境，不阻断主流程。替换既有 PATH= 项而非 append（env 重复项语义
	// 依赖 libc 实现，替换为确定性覆盖）。
	if loginPath, err := clibin.LoginPath(); err == nil && loginPath != "" {
		env := os.Environ()
		replaced := false
		for i, kv := range env {
			if strings.HasPrefix(kv, "PATH=") {
				env[i] = "PATH=" + loginPath
				replaced = true
				break
			}
		}
		if !replaced {
			env = append(env, "PATH="+loginPath)
		}
		cmd.Env = env
	}
	// WaitDelay 收口孙进程管道：ctx 超时只 kill claude 主进程，其内部的 git fetch/clone
	// 孙进程仍继承 stdout/stderr 管道写端——管道不关则 Run() 永久阻塞（超时分支不可达：
	// HTTP 挂死、前端无限转圈、包级 mutex 永久占用）。设置后进程退出且管道仍被持有，
	// os/exec 在 WaitDelay 到期强制关闭管道让 Run() 返回，最坏在超时后 ~5s 内报错收场。
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("claude 命令执行超时（%v）：claude %s", timeout, strings.Join(args, " "))
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("claude 命令执行失败：%s", truncateRunes(detail, 300))
	}
	return stdout.String(), nil
}

// truncateRunes 按 rune 截断并去首尾空白（stderr 摘要防超长刷屏）。
func truncateRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if rs := []rune(s); len(rs) > n {
		return string(rs[:n]) + "…"
	}
	return s
}
