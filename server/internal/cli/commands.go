package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 单命令统一超时：本地回环 + 一次工具调用足够；服务未启动时连接拒绝即刻返回，不会挂满。
const commandTimeout = 30 * time.Second

// runMcp 分发 mcp 命令组（tools / schema / call）。
func runMcp(args []string, streams Streams) int {
	if len(args) == 0 {
		fmt.Fprint(streams.Stderr, mcpUsage)
		return ExitUsage
	}
	switch args[0] {
	case "tools":
		return runMcpTools(streams)
	case "schema":
		return runMcpSchema(args[1:], streams)
	case "call":
		return runMcpCall(args[1:], streams)
	default:
		fmt.Fprintf(streams.Stderr, "未知子命令 %q\n\n", args[0])
		fmt.Fprint(streams.Stderr, mcpUsage)
		return ExitUsage
	}
}

// runMcpTools 列出全部 MCP 工具（完整 Tool JSON：name/description/inputSchema/outputSchema）。
func runMcpTools(streams Streams) int {
	port, code, ok := resolvePortOrReport(streams)
	if !ok {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	var tools []*mcp.Tool
	err := WithSession(ctx, port, func(session *mcp.ClientSession) error {
		var e error
		tools, e = listTools(ctx, session)
		return e
	})
	if err != nil {
		return reportConnError(streams, port, err)
	}
	if tools == nil {
		tools = []*mcp.Tool{} // 空列表输出 [] 而非 null
	}
	return writeJSON(streams, tools)
}

// runMcpSchema 输出指定工具的完整定义（含 inputSchema/outputSchema）。
func runMcpSchema(args []string, streams Streams) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprint(streams.Stderr, "用法: ocean-harness mcp schema <name>\n\n")
		return ExitUsage
	}
	name := args[0]
	port, code, ok := resolvePortOrReport(streams)
	if !ok {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	var tool *mcp.Tool
	err := WithSession(ctx, port, func(session *mcp.ClientSession) error {
		tools, e := listTools(ctx, session)
		if e != nil {
			return e
		}
		tool, _ = findTool(tools, name)
		return nil
	})
	if err != nil {
		return reportConnError(streams, port, err)
	}
	if tool == nil {
		fmt.Fprintf(streams.Stderr, "未找到工具 %q（可先执行 ocean-harness mcp tools 查看全部工具名）\n", name)
		return ExitUsage
	}
	return writeJSON(streams, tool)
}

// runMcpCall 调用指定工具：--data 传 JSON 对象入参（省略视为空对象），stdout 输出结果 JSON。
// 手动解析而非 flag 包：flag 在首个位置参数处停止解析，无法表达「工具名在前、flag 在后」
// （mcp call <name> --data '...'）这一调用形态。
func runMcpCall(args []string, streams Streams) int {
	const usage = "用法: ocean-harness mcp call <name> --data '<json>'\n\n"
	name := ""
	data := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--data" || arg == "-data":
			if i+1 >= len(args) {
				fmt.Fprintf(streams.Stderr, "--data 缺少值\n\n%s", usage)
				return ExitUsage
			}
			data = args[i+1]
			i++
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(streams.Stderr, "未知参数 %q\n\n%s", arg, usage)
			return ExitUsage
		case name == "":
			name = arg
		default:
			fmt.Fprintf(streams.Stderr, "多余的位置参数 %q\n\n%s", arg, usage)
			return ExitUsage
		}
	}
	if name == "" {
		fmt.Fprint(streams.Stderr, usage)
		return ExitUsage
	}

	raw := []byte(strings.TrimSpace(data))
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if !json.Valid(raw) {
		fmt.Fprintf(streams.Stderr, "--data 不是合法 JSON：%s\n", data)
		return ExitUsage
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		fmt.Fprintf(streams.Stderr, "--data 必须是 JSON 对象（形如 '{\"key\":...}'），收到：%s\n", data)
		return ExitUsage
	}

	port, code, ok := resolvePortOrReport(streams)
	if !ok {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	var res *mcp.CallToolResult
	err := WithSession(ctx, port, func(session *mcp.ClientSession) error {
		var e error
		res, e = session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
		return e
	})
	if err != nil {
		return reportConnError(streams, port, err)
	}
	if res.IsError {
		fmt.Fprintln(streams.Stderr, errText(res))
		return ExitToolError
	}
	return writeJSON(streams, callPayload(res))
}

// resolvePortOrReport 解析端口；失败时输出文案并返回 (0, 退出码, false)。
func resolvePortOrReport(streams Streams) (port, code int, ok bool) {
	port, err := resolvePort()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "%v\n", err)
		return 0, ExitUsage, false
	}
	return port, ExitOK, true
}

// reportConnError 报告连接/协议层失败；「服务不可达类」错误附加启动引导文案。一律 exit 2。
func reportConnError(streams Streams, port int, err error) int {
	fmt.Fprintf(streams.Stderr, "调用 MCP 服务失败：%v\n", err)
	if isUnreachable(err) {
		fmt.Fprintf(streams.Stderr,
			"Ocean Harness 本地服务不可达（127.0.0.1:%d）。请先启动 Ocean Harness 桌面应用；若服务端口非默认值，可设置环境变量 %s 后重试（dev 应用注册的命令名为 ocean-harness-dev）。\n",
			port, EnvPort)
	}
	return ExitUsage
}

// isUnreachable 判断错误是否为「服务不可达类」（连接拒绝 / 超时），决定是否附加引导文案；
// 其余错误（如工具不存在）保持原始报错，不误导用户去启动应用。
func isUnreachable(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr) && urlErr.Timeout()
}
