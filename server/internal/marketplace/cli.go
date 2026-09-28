// Package marketplace 插件市场域基础能力：封装 claude plugin CLI 调用（os/exec）与
// marketplace 清单扫描。全部为纯函数；插件安装状态与市场注册表的 SSOT 恒为 claude 侧
// （~/.claude/plugins），本包只做投影与透传，不落任何本地状态。
// 未来接入其他 CLI 开发工具（codex/gemini 等）时：按同包新增 <cli>.go 命令构造文件 +
// 在 manifest.go 的 DetectSupportedClis 追加该 CLI 的清单目录约定即可。
package marketplace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CLI 超时口径：add/update 涉及 git clone（受网络影响）放宽；其余为本地操作。
const (
	timeoutClone = 120 * time.Second
	timeoutLocal = 60 * time.Second
)

// claudePathFallbacks 是 GUI 拉起的 sidecar 缺失 shell PATH 时的兜底探测路径
// （macOS GUI 进程默认 PATH 不含 /usr/local/bin 等前缀）。LookPath 命中则不进入本列表；
// 覆盖 brew（Intel/Apple Silicon）安装布局。
var claudePathFallbacks = []string{"/usr/local/bin/claude", "/opt/homebrew/bin/claude"}

// ResolveClaudeBin 解析 claude 可执行文件路径：先 PATH（LookPath），失败后依次探测
// 常见安装位置（fallback 列表 + ~/.claude/local/claude——官方本地安装脚本布局）。
// 全部失败返回中文错误并列出已尝试位置，便于用户自查安装形态。
func ResolveClaudeBin() (string, error) {
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}
	probes := append([]string{}, claudePathFallbacks...)
	if home, err := os.UserHomeDir(); err == nil {
		probes = append(probes, filepath.Join(home, ".claude", "local", "claude"))
	}
	for _, p := range probes {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", errors.New("未找到 claude 命令：请先安装 Claude Code CLI（已尝试 PATH 与 " + strings.Join(probes, "、") + "）")
}

// run 执行 `claude <args...>`：context 超时兜底、分离捕获 stdout/stderr。
// 非零退出返回中文错误（附 stderr 摘要）；成功返回原始 stdout（JSON 交由调用方解析）。
// 每次调用现查二进制路径（LookPath 为微秒级），保证安装形态变化后无需重启即生效。
func run(timeout time.Duration, args ...string) (string, error) {
	bin, err := ResolveClaudeBin()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
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
