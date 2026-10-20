/*
 * Package mcpservers 内嵌 MCP Server（docs/agent_dev_01_tasks.md T2.1），把 tracker / issueWorkspace 的既有 service 能力与 GitHub 外部服务工具（T4.1）以 MCP 工具形式暴露，供 issue 运行工作空间内的 AI agent（经 T1.3 生成的 .mcp.json 接入）调用。
 * 模式移植自 pros-admin-server/mcp_servers（go-sdk，Streamable HTTP，无鉴权单机场景）。组织方式（单 server 归口——T2.1 原预留的「每业务域一个 server 按路径扩展」框架取消，多端点对调用方有割裂感）：全部工具注册在唯一的 ocean_harness server（本文件 init()），工具名按前缀分组（issue_* / github_*，后续平台/业务同理）；
 * 共用基础设施在 mcp_util/（McpTool 基类与失败日志、app_config 只读读取；工具结果的装配由 SDK ToolHandlerFor 自动完成），工具 handler 实现在 mcp_tool/（文件按业务域命名），入出参 DTO 在 mcp_dto/。注意：同包多文件 init() 按文件名序执行，新工具注册一律追加在本文件 init() 内，不另立 mcp_*.go 的 init（避免字母序在前拿到未初始化的 server 单例）。
 */
package mcpservers

import (
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"ocean-harness/server/internal/mcpservers/mcp_tool"
)

/*
 * mcpServerOceanHarness 是 ocean_harness server 的进程级单例（多会话共享；
 * 会话管理由 StreamableHTTPHandler 负责）。
 * init() 完成全部工具注册：InputSchema/OutputSchema 均由 handler 泛型 In/Out 反射推导（字段描述 = DTO 的 jsonschema tag），不手写 schema。
 */
var mcpServerOceanHarness *mcp.Server

func init() {
	mcpServerOceanHarness = mcp.NewServer(&mcp.Implementation{
		Name:    "ocean_harness",
		Version: "v1.0.0",
		Title:   "ocean-harness 项目管理工具（issue / 子任务 / 工作空间 / GitHub）",
	}, nil)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "issue_get_info",
		Description: "获取 issue 详情（标题、描述、状态、优先级、类型、关联仓库与基准分支、父子关系）。",
	}, mcptool.McpOceanHarnessTool{}.IssueGetInfo)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name: "issue_update",
		Description: "部分更新 issue：stateCode/name/description 仅更新传入的非空字段，其余字段（含类型、" +
			"关联仓库分支、日期）保留原值。stateCode 变化触发完成时间流转（DONE 记录完成时间）；" +
			"父子状态独立变更不级联，唯一联动是全部子任务完成后父任务自动完成。",
	}, mcptool.McpOceanHarnessTool{}.IssueUpdate)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "issue_child_list",
		Description: "列出某 issue 的全部一级子任务（按看板顺序）。目标不能自身是子任务（仅支持一层）。",
	}, mcptool.McpOceanHarnessTool{}.IssueChildList)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name: "issue_child_create",
		Description: "为指定 issue 创建一级子任务（项目/工作空间归属自动继承父任务）。" +
			"name 必填；stateCode 留空默认 BACKLOG。",
	}, mcptool.McpOceanHarnessTool{}.IssueChildCreate)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name: "issue_child_update",
		Description: "部分更新子任务：stateCode/name/description 仅更新传入的非空字段，其余字段保留原值。" +
			"目标必须是子任务（parentId 非空），否则请改用 issue_update。",
	}, mcptool.McpOceanHarnessTool{}.IssueChildUpdate)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name: "issue_workspace_status",
		Description: "查询某 issue 运行工作空间的初始化状态（顶层结论 + createDirs/sshConfig/" +
			"cloneRepos 各步骤与仓库级进度）。workspace 基目录取应用设置，无需传入。",
	}, mcptool.McpOceanHarnessTool{}.WorkspaceStatus)

	// —— 列举工具（workspace / project / issue 清单，供选择类工作流逐级定位）——

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "workspace_list",
		Description: "列出全部工作空间（新建在前）：id/名称/工作区目录/描述。无副作用读。",
	}, mcptool.McpOceanHarnessTool{}.WorkspaceList)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "project_list",
		Description: "列出某工作空间下的全部项目（新建在前）：id/名称/emoji/描述。workspaceId 取 workspace_list 返回的 id。",
	}, mcptool.McpOceanHarnessTool{}.ProjectList)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name: "issue_list",
		Description: "列出某项目下的全部任务（按看板顺序，含子任务，parentId 区分层级）；" +
			"projectId 取 project_list 返回的 id，stateCode 留空返回全部状态。单任务详情请用 issue_get_info。",
	}, mcptool.McpOceanHarnessTool{}.IssueList)

	// —— GitHub 外部服务工具（T4.1，github_ 前缀分组；PAT 经设置 → 个人中心录入）——

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name: "github_create_pr",
		Description: "为本地仓库对应的 GitHub 仓库创建 Pull Request。" +
			"仓库按 localRepositoryId 定位（issue_get_info 的 repositoryBranchList）；" +
			"head 留空默认 agent_{issueId}，base 留空默认该 issue 关联的基准分支；" +
			"需先在 设置 → 个人中心 → GitHub 录入 PAT。",
	}, mcptool.McpGithubTool{}.CreatePullRequest)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "github_list_prs",
		Description: "列出本地仓库对应的 GitHub 仓库的 Pull Request（state 留空默认 open，最多 50 条）。",
	}, mcptool.McpGithubTool{}.ListPullRequests)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "github_ci_status",
		Description: "获取指定 PR 的 CI 检查状态（汇总结论 + 逐项检查：GitHub Actions 与旧 commit status 归并）。",
	}, mcptool.McpGithubTool{}.PullRequestCIStatus)

	/*
	 * —— 浏览器自动化工具（T2.2，browser_ 前缀分组：原生 6 + 转发 17 + 逃生舱 1）——转发类成功结果为引擎原样
	 * Content（截图含图片与落盘路径文本，尾部追加一行归因元数据）；profile 留空默认 default；
	 * 引擎会话自动 ensure，idle 冷启动 1-3s 属预期。操作元素一律基于 browser_snapshot 的 ref（stale ref 重新 snapshot 取新值）。
	 */

	// 原生 6

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_status",
		Description: "查看浏览器会话全局投影（各 profile 的状态 idle/starting/ready/failed、页面列表、最近操作流水）。无副作用读。",
	}, mcptool.McpBrowserTool{}.Status)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name: "browser_profile_open",
		Description: "拉起（或复用）指定 profile 的浏览器并同步等就绪。headless 留空读该 profile 偏好文件，显式传入覆写并回存" +
			"（无头/有头切换会关掉重开，登录态在 profile 目录续存）。冷启动 1-3s 属预期。",
	}, mcptool.McpBrowserTool{}.ProfileOpen)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name: "browser_session_close",
		Description: "关停指定 profile 的浏览器引擎进程（幂等，未运行亦成功）。登录态在 profile 目录续存，下次打开自动恢复。" +
			"注意与 browser_close 区分：那只关闭当前页 tab，不关引擎进程。",
	}, mcptool.McpBrowserTool{}.SessionClose)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_auth_state",
		Description: "拉取浏览器当前登录态（cookie 全集 + 当前页 localStorage）。凭据仅用于本次 browser_auth_request 的入参构造，勿外传勿落盘。",
	}, mcptool.McpBrowserTool{}.AuthState)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name: "browser_auth_request",
		Description: "带登录态的 HTTP 请求：实时拉取浏览器 cookies（按声明域落锚，跨站凭据不泄漏）后直接发送，" +
			"适合调用需要登录态的站点 API。显式 User-Agent 优先于浏览器真实 UA。",
	}, mcptool.McpBrowserTool{}.AuthedRequest)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_tool_list",
		Description: "查询浏览器引擎当前可用的全部工具（browser_tool_call 直调的事实源；服务端黑名单工具带 blocked 标注，直调被拒）。",
	}, mcptool.McpBrowserTool{}.ToolList)

	// 转发 17（引擎原样结果；元素操作基于 browser_snapshot 的 ref）

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_navigate",
		Description: "浏览器打开指定 URL（成功返回页面快照）。",
	}, mcptool.McpBrowserTool{}.Navigate)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_snapshot",
		Description: "获取页面可访问性快照（元素树 + ref 引用）。后续点击/输入等操作一律基于最新快照的 ref；页面变化后重新获取。",
	}, mcptool.McpBrowserTool{}.Snapshot)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name: "browser_click",
		Description: "点击页面元素（target 来自最新快照）。点击后若报 modal state，需先处理模态：" +
			"alert/confirm 对话框用 browser_handle_dialog，文件选择框用 browser_file_upload。",
	}, mcptool.McpBrowserTool{}.Click)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_type",
		Description: "向可编辑元素输入文本（submit 为 true 时输入后按 Enter 提交）。",
	}, mcptool.McpBrowserTool{}.Type)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_fill_form",
		Description: "批量填写多个表单字段（文本框/复选框/单选/下拉/滑杆），比逐个 browser_type 高效。",
	}, mcptool.McpBrowserTool{}.FillForm)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_select_option",
		Description: "选择下拉框的选项（可单选可多选）。",
	}, mcptool.McpBrowserTool{}.SelectOption)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_wait_for",
		Description: "等待页面状态：指定文本出现/消失，或等待固定秒数（至多 30s）。time/text/textGone 至少传一。",
	}, mcptool.McpBrowserTool{}.WaitFor)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name: "browser_evaluate",
		Description: "在页面（或指定元素）上执行 JavaScript 函数并返回结果。" +
			"从 localStorage 提取 token 等场景配合 browser_auth_state 使用。",
	}, mcptool.McpBrowserTool{}.Evaluate)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_tabs",
		Description: "管理浏览器 tab：list 列表/new 新建（可带 url）/close 关闭/select 选中。",
	}, mcptool.McpBrowserTool{}.Tabs)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_take_screenshot",
		Description: "截取当前页面（或指定元素）为图片，结果含图像与落盘路径。截图不可用于继续操作，操作需基于 browser_snapshot 的 ref。",
	}, mcptool.McpBrowserTool{}.Screenshot)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_file_upload",
		Description: "处理页面文件选择框（点击上传按钮出现 chooser 后调用）；paths 留空即取消选择。",
	}, mcptool.McpBrowserTool{}.FileUpload)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_console_messages",
		Description: "读取页面控制台消息（level 为级别下限，默认 info；all 为 true 返回整个会话以来的消息）。",
	}, mcptool.McpBrowserTool{}.ConsoleMessages)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_navigate_back",
		Description: "浏览器历史后退一步。",
	}, mcptool.McpBrowserTool{}.NavigateBack)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_close",
		Description: "关闭当前页 tab（浏览器保持运行，后续操作自动重开页）。注意与 browser_session_close 区分：那是关停整个引擎进程。",
	}, mcptool.McpBrowserTool{}.Close)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_find",
		Description: "在页面快照中按文本或正则查找元素及其 ref（比整页 snapshot 省时）；text/regex 恰传其一。",
	}, mcptool.McpBrowserTool{}.Find)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_press_key",
		Description: "按下键盘按键（如 ArrowLeft/Enter）。Enter 提交的输入框可直接用此键，或用 browser_type 的 submit。",
	}, mcptool.McpBrowserTool{}.PressKey)

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name:        "browser_handle_dialog",
		Description: "处理页面对话框（alert/confirm/prompt）。页面报 dialog modal state 时这是唯一清除工具，accept 必填。",
	}, mcptool.McpBrowserTool{}.HandleDialog)

	// 逃生舱 1

	mcp.AddTool(mcpServerOceanHarness, &mcp.Tool{
		Name: "browser_tool_call",
		Description: "直调浏览器引擎任意非黑名单工具（名单见 browser_tool_list）：name 传引擎工具实名，args 按该工具引擎 schema 传。" +
			"引擎结果原样直通（错误不包装）；browser_run_code_unsafe 等可在引擎进程内执行任意代码的黑名单工具被拒绝。" +
			"优先使用上面的专用 browser_* 工具，本工具仅作兜底。",
	}, mcptool.McpBrowserTool{}.ToolCall)
}

/*
 * mcpSessionTimeout 是流式 HTTP 会话的空闲 TTL：客户端异常退出（来不及 DELETE
 * 终止会话）时，服务端自动回收会话资源，不再留永久孤儿；正常路径 CLI 仍主动 DELETE 即时释放，TTL 只是兜底。
 */
const mcpSessionTimeout = 10 * time.Minute

/*
 * McpOceanHarnessStreamableHTTPHandler 构造 ocean_harness server 的
 * Streamable HTTP handler（由 router 调用一次；有状态会话 + SSE 流式响应，空闲 10 分钟自动回收）。
 */
func McpOceanHarnessStreamableHTTPHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServerOceanHarness
	}, &mcp.StreamableHTTPOptions{SessionTimeout: mcpSessionTimeout})
}
