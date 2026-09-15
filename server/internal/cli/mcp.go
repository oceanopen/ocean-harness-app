package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// newMcpCmd 构建 mcp 命令组（tools / schema / call）。
func newMcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "直连本机应用的 MCP 工具",
		// 未知子命令不交给 cobra 默认报错，统一走 groupRunE 中文文案 + 相似命令建议
		Args: cobra.ArbitraryArgs,
		RunE: groupRunE,
	}
	cmd.AddCommand(newMcpToolsCmd(), newMcpSchemaCmd(), newMcpCallCmd())
	return cmd
}

// newMcpToolsCmd 列出全部 MCP 工具（完整 Tool JSON：name/description/inputSchema/outputSchema）。
func newMcpToolsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "列出全部 MCP 工具（JSON）",
		Args:  noArgs,
		RunE:  runMcpTools,
	}
}

// newMcpSchemaCmd 输出指定工具的完整定义（含 inputSchema/outputSchema）。
func newMcpSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema <name>",
		Short: "查看工具 schema（JSON）",
		Args:  exactArgsN(1),
		RunE:  runMcpSchema,
	}
}

// newMcpCallCmd 调用指定工具：--data 传 JSON 对象入参（省略视为空对象），stdout 输出结果 JSON。
func newMcpCallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "call <name>",
		Short: "调用工具（stdout 输出结果 JSON）",
		Args:  exactArgsN(1),
		RunE:  runMcpCall,
	}
	cmd.Flags().String("data", "",
		`工具入参 JSON 对象：内联 '{"key":...}'、@path/file.json 读文件、- 读 stdin；省略视为 {}`)
	return cmd
}

// runMcpTools 列出全部工具。
func runMcpTools(cmd *cobra.Command, args []string) error {
	var tools []*mcp.Tool
	err := withMcpSession(cmd, func(ctx context.Context, session *mcp.ClientSession) error {
		var e error
		tools, e = listTools(ctx, session)
		return e
	})
	if err != nil {
		return err
	}
	if tools == nil {
		tools = []*mcp.Tool{} // 空列表输出 [] 而非 null
	}
	return writeJSON(cmd, tools)
}

// findTool 按名查找工具；schema 命令用于校验未知工具名。
func findTool(tools []*mcp.Tool, name string) (*mcp.Tool, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return nil, false
}

// runMcpSchema 输出指定工具的完整定义。
func runMcpSchema(cmd *cobra.Command, args []string) error {
	name := args[0]
	var tool *mcp.Tool
	err := withMcpSession(cmd, func(ctx context.Context, session *mcp.ClientSession) error {
		tools, e := listTools(ctx, session)
		if e != nil {
			return e
		}
		tool, _ = findTool(tools, name)
		return nil
	})
	if err != nil {
		return err
	}
	if tool == nil {
		return usageErr("未找到工具 %q（可先执行 %s mcp tools 查看全部工具名）", name, commandName())
	}
	return writeJSON(cmd, tool)
}

// runMcpCall 调用指定工具。pflag 原生支持 flag 后置于位置参数（call <name> --data ...），
// 替代旧实现的手写解析循环。
func runMcpCall(cmd *cobra.Command, args []string) error {
	name := args[0]
	// parseCallData 先于端口校验：--data 非法时优先报用法错误，不触网
	arguments, err := parseCallData(cmd)
	if err != nil {
		return usageErr("%v", err)
	}

	var res *mcp.CallToolResult
	err = withMcpSession(cmd, func(ctx context.Context, session *mcp.ClientSession) error {
		var e error
		res, e = session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
		return e
	})
	if err != nil {
		return err
	}
	if res.IsError {
		return toolErr(errText(res))
	}
	return writeJSON(cmd, callPayload(res))
}

// parseCallData 解析并校验 --data：三种来源 → 原始字节 → 必须为 JSON 对象。
func parseCallData(cmd *cobra.Command) (map[string]any, error) {
	raw, err := dataArgBytes(cmd.Flag("data").Value.String(), cmd.InOrStdin())
	if err != nil {
		return nil, err
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("--data 不是合法 JSON：%s", summarize(raw))
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, fmt.Errorf("--data 必须是 JSON 对象（形如 '{\"key\":...}'），收到：%s", summarize(raw))
	}
	return arguments, nil
}

// dataArgBytes 解析 --data 三种来源：空 → {}；- → stdin；@path → 文件；其余 → 内联原文。
func dataArgBytes(raw string, stdin io.Reader) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	switch {
	case trimmed == "":
		return []byte("{}"), nil
	case trimmed == "-":
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("读取 stdin 失败：%v", err)
		}
		return b, nil
	case strings.HasPrefix(trimmed, "@"):
		path := strings.TrimPrefix(trimmed, "@")
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取 --data 文件 %s 失败：%v", path, err)
		}
		return b, nil
	default:
		return []byte(trimmed), nil
	}
}

// summarize 截断 --data 回显长度，避免大文件误用时刷屏。
func summarize(raw []byte) string {
	const max = 200
	if len(raw) > max {
		return string(raw[:max]) + "…（已截断）"
	}
	return string(raw)
}
