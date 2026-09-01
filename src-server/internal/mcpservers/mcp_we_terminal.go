// Package mcpservers 内嵌 MCP Server（docs/agent_dev_01_tasks.md T2.1），把 tracker /
// issueWorkspace 的既有 service 能力以 MCP 工具形式暴露，供 issue 运行工作空间内的 AI
// agent（经 T1.3 生成的 .mcp.json 接入）调用。模式移植自 pros-admin-server/mcp_servers
// （go-sdk v0.2.0，Streamable HTTP，无鉴权单机场景）。
//
// 组织方式（多 server 并存的通用框架，后续 T4.1 github / 第三方对接按同构扩展）：
// 根目录每业务域一个 mcp_<server>.go（Server 定义 + 工具注册 + Handler 工厂），路由按
// server 扩展 /mcp/streamableHttp/<serverName>（见 router.go）；共用基础设施在 mcp_util/
// （McpTool 基类、McpOK/McpFail 结果包装、app_config 只读读取），工具 handler 实现在
// mcp_tool/（文件按 server 命名），入出参 DTO 在 mcp_dto/。
package mcpservers

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"we-claude-terminal/go-server/internal/mcpservers/mcp_tool"
)

// mcpServerWeTerminal 是 we_terminal server 的进程级单例（多会话共享；会话管理由
// StreamableHTTPHandler 负责）。init() 完成全部工具注册：InputSchema/OutputSchema 均由
// handler 泛型 Args/Content 反射推导（字段描述 = DTO 的 jsonschema tag），不手写 schema。
var mcpServerWeTerminal *mcp.Server

func init() {
	mcpServerWeTerminal = mcp.NewServer(&mcp.Implementation{
		Name:    "we_terminal",
		Version: "v1.0.0",
		Title:   "we-claude-terminal 项目管理工具（issue / 子任务 / 工作空间）",
	}, nil)

	mcp.AddTool(mcpServerWeTerminal, &mcp.Tool{
		Name:        "issue_get_info",
		Description: "获取 issue 详情（标题、描述、状态、优先级、标签、关联仓库与基准分支、父子关系）。",
	}, mcptool.McpWeTerminalTool{}.IssueGetInfo)

	mcp.AddTool(mcpServerWeTerminal, &mcp.Tool{
		Name: "issue_update",
		Description: "部分更新 issue：stateCode/name/description 仅更新传入的非空字段，其余字段（含标签、" +
			"关联仓库分支、日期）保留原值。stateCode 变化触发完成时间流转与父子状态联动" +
			"（父任务状态变化级联子任务；全部子任务完成后父任务自动完成）。",
	}, mcptool.McpWeTerminalTool{}.IssueUpdate)

	mcp.AddTool(mcpServerWeTerminal, &mcp.Tool{
		Name:        "issue_child_list",
		Description: "列出某 issue 的全部一级子任务（按看板顺序）。目标不能自身是子任务（仅支持一层）。",
	}, mcptool.McpWeTerminalTool{}.IssueChildList)

	mcp.AddTool(mcpServerWeTerminal, &mcp.Tool{
		Name: "issue_child_create",
		Description: "为指定 issue 创建一级子任务（项目/工作空间归属自动继承父任务）。" +
			"name 必填；stateCode 留空默认 BACKLOG。",
	}, mcptool.McpWeTerminalTool{}.IssueChildCreate)

	mcp.AddTool(mcpServerWeTerminal, &mcp.Tool{
		Name: "issue_child_update",
		Description: "部分更新子任务：stateCode/name/description 仅更新传入的非空字段，其余字段保留原值。" +
			"目标必须是子任务（parentId 非空），否则请改用 issue_update。",
	}, mcptool.McpWeTerminalTool{}.IssueChildUpdate)

	mcp.AddTool(mcpServerWeTerminal, &mcp.Tool{
		Name: "issue_workspace_status",
		Description: "查询某 issue 运行工作空间的初始化状态（顶层结论 + createDirs/sshConfig/" +
			"cloneRepos 各步骤与仓库级进度）。workspace 基目录取应用设置，无需传入。",
	}, mcptool.McpWeTerminalTool{}.WorkspaceStatus)
}

// McpWeTerminalStreamableHTTPHandler 构造 we_terminal server 的 Streamable HTTP handler
// （由 router 调用一次；v0.2.0 的 options 为空占位结构，nil 即默认：有状态会话 + SSE 流式响应）。
func McpWeTerminalStreamableHTTPHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServerWeTerminal
	}, nil)
}
