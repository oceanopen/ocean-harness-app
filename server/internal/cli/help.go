package cli

// 命令名常量（app.go / commands.go 的分发表共用）。
const (
	cmdMcp     = "mcp"
	cmdVersion = "version"
)

const helpText = `ocean-harness — Ocean Harness 命令行（直连本机应用的 MCP 工具）

用法:
  ocean-harness mcp tools                        列出全部 MCP 工具（JSON）
  ocean-harness mcp schema <name>                查看工具 schema（JSON，含 inputSchema/outputSchema）
  ocean-harness mcp call <name> --data '<json>'  调用工具（stdout 输出结果 JSON）
  ocean-harness version                          版本与构建模式

环境:
  OCEAN_HARNESS_PORT  覆盖服务端口（默认 dev=9000 / release=9100）

退出码: 0 成功 | 1 工具返回业务错误 | 2 用法或连接错误
`

// mcpUsage mcp 子命令组的使用说明（stderr 报错后附带）。
const mcpUsage = `用法:
  ocean-harness mcp tools                        列出全部 MCP 工具（JSON）
  ocean-harness mcp schema <name>                查看工具 schema（JSON）
  ocean-harness mcp call <name> --data '<json>'  调用工具（stdout 输出结果 JSON）
`
