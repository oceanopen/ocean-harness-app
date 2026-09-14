// Package cli 实现 ocean-harness 命令行：以 MCP client 身份直连本机 Go sidecar 的
// /mcp/streamableHttp/oceanHarness 端点（与 Claude 插件所见完全同构，本地调试所见即所得）。
//
// 输出契约（unix 惯例）：stdout 只出数据（纯 JSON），stderr 只出人类可读文案；
// 退出码 0 成功 / 1 工具返回业务错误（IsError）/ 2 用法错误或服务不可达。
// 未来 task 等新命名空间 = 在 Run 的命令表追加分支，模式与本组一致。
package cli

import (
	"fmt"
	"io"

	"ocean-harness/server/internal/buildinfo"
)

// Streams 命令输出流。main 传 os 标准流；测试注入 bytes.Buffer 做 golden 断言。
type Streams struct {
	Stdout io.Writer
	Stderr io.Writer
}

// 退出码契约。
const (
	ExitOK        = 0 // 成功
	ExitToolError = 1 // 工具返回业务错误（IsError=true，stderr 为服务端中文文案）
	ExitUsage     = 2 // 用法错误或服务不可达（环境问题）
)

// Run 解析 args（= os.Args[1:]）并执行对应命令，返回进程退出码。
func Run(args []string, streams Streams) int {
	if len(args) == 0 {
		fmt.Fprint(streams.Stderr, helpText)
		return ExitUsage
	}
	switch args[0] {
	case cmdMcp:
		return runMcp(args[1:], streams)
	case cmdVersion:
		fmt.Fprintf(streams.Stdout, "ocean-harness-cli %s (mode: %s)\n", buildinfo.Version, buildinfo.Mode)
		return ExitOK
	case "-h", "--help", "help":
		fmt.Fprint(streams.Stdout, helpText)
		return ExitOK
	default:
		fmt.Fprintf(streams.Stderr, "未知命令 %q\n\n", args[0])
		fmt.Fprint(streams.Stderr, helpText)
		return ExitUsage
	}
}
