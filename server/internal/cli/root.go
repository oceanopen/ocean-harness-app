// Package cli 实现 ocean-harness CLI：以 MCP client 身份直连本机 Go sidecar 的
// /mcp/streamableHttp/oceanHarness 端点（与 Claude 插件所见完全同构，本地调试所见即所得）。
//
// 命令层基于 spf13/cobra，惯用法形态：constructor 函数构建命令树，Execute 为 main 专用
// 入口；命令树每次调用重建，无包级可变状态（测试用 newRootCmd + SetArgs/SetOut/SetErr）。
// 输出契约（unix 惯例）：stdout 只出数据（纯 JSON），stderr 只出人类可读文案；退出码
// 0 成功 / 1 工具返回业务错误（IsError）/ 2 用法错误或服务不可达。
// SilenceErrors/SilenceUsage 关闭 cobra 默认英文报错——错误文案与退出码统一经 exitError
// 携带、由 mappedExitCode 输出。直连 sidecar 的会话编排与连接错误分类独立在 session.go
// （withMcpSession/connErr），各命名空间共用。未来 task 等新命名空间 = 新增 <ns>.go 并在
// newRootCmd 注册一行，模式与 mcp.go 一致。
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"ocean-harness/server/internal/buildinfo"
)

// 退出码契约。
const (
	ExitOK        = 0 // 成功
	ExitToolError = 1 // 工具返回业务错误（IsError=true，stderr 为服务端中文文案）
	ExitUsage     = 2 // 用法错误或服务不可达（环境问题）
)

// exitError 携带退出码与人类可读文案穿过 cobra 的 Execute error 通道；
// 命令内只构造不输出，文案统一由 mappedExitCode 打印到 stderr。
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

// usageErr 构造用法类错误（exit 2）。
func usageErr(format string, a ...any) error {
	return &exitError{code: ExitUsage, msg: fmt.Sprintf(format, a...)}
}

// toolErr 构造工具业务错误（exit 1）。
func toolErr(msg string) error {
	return &exitError{code: ExitToolError, msg: msg}
}

// commandName 本构建的注册命令名（与 Rust cli_register::link_name 的 symlink 名同构，
// 勿单方修改）：test 模式 = ocean-harness-dev-cli，release = ocean-harness-cli。
func commandName() string {
	if buildinfo.Mode == "release" {
		return "ocean-harness-cli"
	}
	return "ocean-harness-dev-cli"
}

// 中文帮助模板（Usage/Flags/Examples 标签中文化；cobra 的 UsageFunc/HelpFunc 沿父链
// 继承，root 上设置一次即对整棵命令树生效）。
const chineseUsageTemplate = `用法:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [命令]{{end}}{{if gt (len .Aliases) 0}}

别名:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

示例:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

可用命令:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

参数:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

全局参数:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

执行 "{{.CommandPath}} [命令] --help" 查看某一命令的详细帮助。{{end}}
`

const chineseHelpTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`

// newRootCmd 构建命令树（每次调用全新实例，供 Execute 与测试共用）。
func newRootCmd() *cobra.Command {
	name := commandName()
	root := &cobra.Command{
		Use:   name,
		Short: "Ocean Harness 命令行（直连本机应用的 MCP 工具）",
		Long: fmt.Sprintf(`%s — Ocean Harness 命令行（直连本机应用的 MCP 工具）。

以 MCP client 身份直连本机 Go sidecar 的 MCP 端点，与 Claude 插件所见完全同构：
先在终端调通，再交给大模型使用。

环境:
  %s  覆盖服务端口（--port 参数优先；默认 dev=9000 / release=9100）

退出码: 0 成功 | 1 工具返回业务错误 | 2 用法或连接错误`, name, EnvPort),
		Example: fmt.Sprintf(`  %[1]s mcp tools                        列出全部 MCP 工具（JSON）
  %[1]s mcp schema <name>                查看工具 schema（JSON，含 inputSchema/outputSchema）
  %[1]s mcp call <name> --data '<json>'  调用工具；--data 亦支持 @file 或 -（stdin）
  %[1]s version                          版本与构建模式`, name),
		Version:       buildinfo.Version,
		SilenceErrors: true, // 关闭 cobra 默认英文报错，文案经 exitError 由 mappedExitCode 统一输出
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs, // 未知命令落入 RunE 自行处理（中文文案 + 相似命令建议）
		RunE:          groupRunE,
	}
	root.SetUsageTemplate(chineseUsageTemplate)
	root.SetHelpTemplate(chineseHelpTemplate)
	// --version 输出与 version 子命令保持一致（版本 + 构建模式，同源于 versionLine）
	root.SetVersionTemplate(versionLine())
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return usageErr("参数错误：%v\n\n%s", err, c.UsageString())
	})
	root.SetHelpCommand(newHelpCmd())
	root.PersistentFlags().Int("port", 0,
		"覆盖目标服务端口（优先级：--port > 环境变量 "+EnvPort+" > 构建模式默认 9000/9100）")
	root.AddCommand(newMcpCmd(), newVersionCmd())
	// 中文化自动注入的 help/version flag 描述（cobra 默认英文 "help for ..."）。
	// completion 子命令在 Execute 期才注入，其内建描述保持英文，属已知取舍。
	var initFlagUsage func(c *cobra.Command)
	initFlagUsage = func(c *cobra.Command) {
		c.InitDefaultHelpFlag()
		c.InitDefaultVersionFlag()
		if f := c.Flags().Lookup("help"); f != nil {
			f.Usage = "查看 " + c.CommandPath() + " 帮助"
		}
		if f := c.Flags().Lookup("version"); f != nil {
			f.Usage = "输出版本与构建模式"
		}
		for _, sub := range c.Commands() {
			initFlagUsage(sub)
		}
	}
	initFlagUsage(root)
	return root
}

// groupRunE 组命令（root / mcp 及未来 task 等命名空间）共用：裸调用输出组用法
// 到 stderr 并返回用法错误；未知子命令给中文文案 + 相似命令建议。
func groupRunE(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return usageErr("%s", cmd.UsageString())
	}
	return unknownCommandErr(cmd, args[0])
}

// unknownCommandErr 未知命令：附相似命令建议（cobra 编辑距离建议）与总用法。
func unknownCommandErr(cmd *cobra.Command, name string) error {
	msg := fmt.Sprintf("未知命令 %q", name)
	if sugg := cmd.SuggestionsFor(name); len(sugg) > 0 {
		quoted := make([]string, 0, len(sugg))
		for _, s := range sugg {
			quoted = append(quoted, cmd.CommandPath()+" "+s)
		}
		msg += "\n您是不是想执行：" + strings.Join(quoted, "、")
	}
	return usageErr("%s\n\n%s", msg, cmd.UsageString())
}

// newHelpCmd 中文版 help 子命令。Find 对非 nil Args 的命令树恒返回最近命令（无错误），
// 未知主题自然回落到 root 帮助（与上游 cobra 在 root.Args 非 nil 时行为一致）。
func newHelpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "help [命令]",
		Short: "查看命令帮助",
		Run: func(c *cobra.Command, args []string) {
			cmd, _, _ := c.Root().Find(args)
			cmd.InitDefaultHelpFlag()
			cmd.InitDefaultVersionFlag()
			_ = cmd.Help()
		},
	}
}

// versionLine 版本输出行（root --version 模板与 version 子命令共用，防漂移；
// 两者输出一致性另有测试回归锚）。
func versionLine() string {
	return fmt.Sprintf("%s %s (mode: %s)\n", commandName(), buildinfo.Version, buildinfo.Mode)
}

// newVersionCmd version 子命令：版本与构建模式（与 root --version 输出一致）。
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "版本与构建模式",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), versionLine())
			return nil
		},
	}
}

// noArgs 不接受位置参数（报错文案中文化并附该命令用法）。
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageErr("未知参数 %q\n\n%s", args[0], cmd.UsageString())
	}
	return nil
}

// exactArgsN 要求恰好 n 个位置参数（报错文案中文化并附该命令用法）。
func exactArgsN(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return usageErr("%s 需要 %d 个位置参数，收到 %d 个\n\n%s", cmd.UseLine(), n, len(args), cmd.UsageString())
		}
		return nil
	}
}

// Execute CLI 主入口：解析 os.Args 并执行命令，返回进程退出码（main 只做 os.Exit）。
func Execute() int {
	root := newRootCmd()
	root.SetArgs(os.Args[1:])
	return mappedExitCode(root, root.Execute())
}

// mappedExitCode 把 cobra Execute 的 error 映射为进程退出码并输出文案：
// nil → 0；exitError → 打印 msg、返回其码；其余（cobra 内建错误兜底）→ 打印原文、返回 2。
func mappedExitCode(root *cobra.Command, err error) int {
	if err == nil {
		return ExitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		if ee.msg != "" {
			fmt.Fprintln(root.ErrOrStderr(), strings.TrimRight(ee.msg, "\n"))
		}
		return ee.code
	}
	fmt.Fprintln(root.ErrOrStderr(), err)
	return ExitUsage
}
