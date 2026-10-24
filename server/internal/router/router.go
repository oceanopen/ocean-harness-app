// Package router 组装 gin engine：中间件 + 路由。
package router

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/controller"
	"ocean-harness/server/internal/mcpservers"
	"ocean-harness/server/internal/middleware"
)

/*
 * SetupRouter 构造 gin engine：注册中间件与路由。
 * 暴露 /api/baseInfo、/api/localRepository、/api/workspace、/api/taskWorkspace、/api/pluginMarketplace、/api/plugin、/api/agentCatalog、/api/doctor、/api/acpSession、/api/browserSession 与 /mcp/streamableHttp/*（MCP 端点，供工作空间内 AI agent 调用），均无需登录/鉴权。
 * gin.SetMode 已在 config.MustLoad 中按环境变量完成。
 */
func SetupRouter() *gin.Engine {
	r := gin.New()

	/*
	 * 中间件：请求访问日志 + panic 恢复 + CORS（webview origin 与 127.0.0.1 不同源）。
	 * AccessLog 须在 Recovery 之前：handler panic 被 Recovery 兜住返回 500 后，访问日志仍能记录完整 status 与 latency。
	 */
	r.Use(middleware.AccessLog())
	r.Use(middleware.Recovery())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: false, // 与 AllowOrigins:* 配合须为 false
		MaxAge:           12 * time.Hour,
	}))

	/*
	 * 路由按命名空间分组：/api/baseInfo（系统信息）；顶层资源独立分组（/api/localRepository、/api/workspace 等），
	 * 随属域资源嵌套（/api/project/task、/api/project/type、/api/project/taskWorkspace 等）。
	 */
	apiGroup := r.Group("/api")
	{
		baseInfoGroup := apiGroup.Group("/baseInfo")
		{
			baseInfoGroup.GET("/getServerRunInfo", controller.BaseInfo{}.GetServerRunInfo)
		}

		/*
		 * localRepository 模块：本地仓库 CRUD + git 刷新（action 风格，POST）。
		 * 本地仓库为顶层资源（无 workspace 归属），独立分组。
		 */
		localRepositoryGroup := apiGroup.Group("/localRepository")
		{
			localRepositoryGroup.POST("/getList", controller.LocalRepository{}.GetList)
			localRepositoryGroup.POST("/getInfo", controller.LocalRepository{}.GetInfo)
			localRepositoryGroup.POST("/create", controller.LocalRepository{}.Create)
			localRepositoryGroup.POST("/update", controller.LocalRepository{}.Update)
			localRepositoryGroup.POST("/delete", controller.LocalRepository{}.Delete)
			localRepositoryGroup.POST("/refresh", controller.LocalRepository{}.Refresh)
			localRepositoryGroup.POST("/refreshAll", controller.LocalRepository{}.RefreshAll)
			localRepositoryGroup.POST("/getLocalBranches", controller.LocalRepository{}.GetLocalBranches)
			localRepositoryGroup.POST("/getWorktreeList", controller.LocalRepository{}.GetWorktreeList)
		}

		/*
		 * pluginMarketplace 模块：插件市场投影与注册表操作（action 风格，POST）。
		 * 无本地表：市场注册表与安装状态的 SSOT 恒为 claude 侧，实时投影（调 claude plugin CLI + 扫描市场清单）。
		 * 与 localRepository 同为顶层资源，独立分组。
		 */
		pluginMarketplaceGroup := apiGroup.Group("/pluginMarketplace")
		{
			pluginMarketplaceGroup.POST("/getList", controller.PluginMarketplace{}.GetList)
			pluginMarketplaceGroup.POST("/add", controller.PluginMarketplace{}.Add)
			pluginMarketplaceGroup.POST("/update", controller.PluginMarketplace{}.Update)
			pluginMarketplaceGroup.POST("/remove", controller.PluginMarketplace{}.Remove)
		}

		/*
		 * agentCatalog 模块：ACP agent 目录只读投影（action 风格，POST）。
		 * 无本地表：SSOT 为 sidecar 内嵌离线快照（internal/agentcatalog）；
		 * 前端下拉可选面 = enabled 条目（launch_settings.agentCode 值域 SSOT，T1.2）。
		 */
		agentCatalogGroup := apiGroup.Group("/agentCatalog")
		{
			agentCatalogGroup.POST("/getList", controller.AgentCatalog{}.GetList)
		}

		/*
		 * doctor 模块：ACP agent doctor 探测（action 风格，POST）。check 异步受理真实握手
		 * 探测（在跑单飞 join），getInfo 轮询读四态快照；SSOT 为 acpdoctor 进程内缓存（T1.4）。
		 */
		doctorGroup := apiGroup.Group("/doctor")
		{
			doctorGroup.POST("/check", controller.Doctor{}.Check)
			doctorGroup.POST("/getInfo", controller.Doctor{}.GetInfo)
		}

		/*
		 * acpSession 模块：ACP 会话域（action 风格，POST；events 为 GET SSE）。会话生命周期
		 * 与事件流的 SSOT 为 global.AcpSessions（T1.5）：ensure 幂等受理建会话，prompt/cancel/
		 * respond* 会话操作，getInfo 轮询读会话视图快照，events 订阅增量事件流（建连即快照）。
		 */
		acpSessionGroup := apiGroup.Group("/acpSession")
		{
			acpSessionGroup.POST("/ensure", controller.AcpSession{}.Ensure)
			acpSessionGroup.POST("/getInfo", controller.AcpSession{}.GetInfo)
			acpSessionGroup.POST("/prompt", controller.AcpSession{}.Prompt)
			acpSessionGroup.POST("/cancel", controller.AcpSession{}.Cancel)
			acpSessionGroup.POST("/respondPermission", controller.AcpSession{}.RespondPermission)
			acpSessionGroup.POST("/respondElicitation", controller.AcpSession{}.RespondElicitation)
			acpSessionGroup.GET("/events", controller.AcpSession{}.Events)
		}
		// conversation 模块：会话实体 CRUD + 终端锚回写（action 风格，POST）。
		conversationGroup := apiGroup.Group("/conversation")
		{
			conversationGroup.POST("/getList", controller.Conversation{}.GetList)
			conversationGroup.POST("/getInfo", controller.Conversation{}.GetInfo)
			conversationGroup.POST("/create", controller.Conversation{}.Create)
			conversationGroup.POST("/update", controller.Conversation{}.Update)
			conversationGroup.POST("/delete", controller.Conversation{}.Delete)
			conversationGroup.POST("/setClaudeSessionId", controller.Conversation{}.SetClaudeSessionID)
		}

		/*
		 * browserSession 模块：浏览器会话域（action 风格，POST；events/stream 为 GET SSE）。per-profile 引擎会话生命周期与投影事件流的 SSOT 为 global.Browser（T2.1）：
		 * ensure 拉起/复用（同步等就绪，headless D10 覆盖）、navigate/analyze 页面操作（引擎结果原样透传；analyze 即时受理页面分析入 ACP 会话，不排队）、getInfo 轮询读全量快照、events 订阅投影事件流（建连即快照，全局单源无绑定）。
		 * stream/input 实时画面帧流与远程桌面输入（CDP 观察通道，T4.5）。
		 */
		browserSessionGroup := apiGroup.Group("/browserSession")
		{
			browserSessionGroup.POST("/getInfo", controller.BrowserSession{}.GetInfo)
			browserSessionGroup.POST("/ensure", controller.BrowserSession{}.Ensure)
			browserSessionGroup.POST("/close", controller.BrowserSession{}.Close)
			browserSessionGroup.POST("/navigate", controller.BrowserSession{}.Navigate)
			browserSessionGroup.POST("/analyze", controller.BrowserSession{}.Analyze)
			browserSessionGroup.POST("/input", controller.BrowserSession{}.Input)
			browserSessionGroup.GET("/events", controller.BrowserSession{}.Events)
			browserSessionGroup.GET("/stream", controller.BrowserSession{}.Stream)
		}

		/*
		 * plugin 模块：按开发工具（cli）维度的插件安装操作（install/uninstall/enable/disable/
		 * update，action 风格 POST）。cli 字段为多 CLI 扩展口子（v1 仅 claude），写操作统一
		 * 返回最新列表投影，前端一次往返即完成刷新。
		 */
		pluginGroup := apiGroup.Group("/plugin")
		{
			pluginGroup.POST("/install", controller.Plugin{}.Install)
			pluginGroup.POST("/uninstall", controller.Plugin{}.Uninstall)
			pluginGroup.POST("/enable", controller.Plugin{}.Enable)
			pluginGroup.POST("/disable", controller.Plugin{}.Disable)
			pluginGroup.POST("/update", controller.Plugin{}.Update)
		}

		/*
		 * imBot 模块：IM 渠道数字人 bot 管理（action 风格，POST）。渠道无关命名（channel 差异
		 * 在 credential JSON 内）；写操作落库后经 supervisor 热更新连接（会话绑定除外，每回合
		 * 现读无需热重载），getList 合并运行态。
		 */
		imBotGroup := apiGroup.Group("/imBot")
		{
			imBotGroup.POST("/getList", controller.ImBot{}.GetList)
			imBotGroup.POST("/getInfo", controller.ImBot{}.GetInfo)
			imBotGroup.POST("/create", controller.ImBot{}.Create)
			imBotGroup.POST("/update", controller.ImBot{}.Update)
			imBotGroup.POST("/delete", controller.ImBot{}.Delete)
			imBotGroup.POST("/restart", controller.ImBot{}.Restart)
			imBotGroup.POST("/getConversations", controller.ImBot{}.GetConversations)
			imBotGroup.POST("/provisionBegin", controller.ImBot{}.ProvisionBegin)
			imBotGroup.POST("/provisionPoll", controller.ImBot{}.ProvisionPoll)
			imBotGroup.POST("/provisionCancel", controller.ImBot{}.ProvisionCancel)
			imBotGroup.POST("/provisionVerifyCode", controller.ImBot{}.ProvisionVerifyCode)
		}

		// project 模块：项目实体 CRUD（action 风格，POST）。项目为顶层资源（独立实体线），独立分组。
		projectGroup := apiGroup.Group("/project")
		{
			projectGroup.POST("/getList", controller.Project{}.GetList)
			projectGroup.POST("/getInfo", controller.Project{}.GetInfo)
			projectGroup.POST("/create", controller.Project{}.Create)
			projectGroup.POST("/update", controller.Project{}.Update)
			projectGroup.POST("/delete", controller.Project{}.Delete)
		}

		/*
		 * taskWorkspace 模块：任务运行工作空间编排（init 异步受理 + status 轮询读状态文件）、
		 * 归档（archive 删目录）、文件浏览（getFileTree/getFileContent + fileRaw 图片字节直连）、
		 * Git 变更（getGitChanges/getFileDiff 未提交变更列表与内容对）。挂 project 下（任务域随项目）。
		 */
		taskWorkspaceGroup := projectGroup.Group("/taskWorkspace")
		{
			taskWorkspaceGroup.POST("/init", controller.TaskWorkspace{}.Init)
			taskWorkspaceGroup.POST("/status", controller.TaskWorkspace{}.Status)
			taskWorkspaceGroup.POST("/archive", controller.TaskWorkspace{}.Archive)
			taskWorkspaceGroup.POST("/getFileTree", controller.TaskWorkspace{}.FileTree)
			taskWorkspaceGroup.POST("/getFileContent", controller.TaskWorkspace{}.FileContent)
			taskWorkspaceGroup.GET("/fileRaw", controller.TaskWorkspace{}.FileRaw)
			taskWorkspaceGroup.POST("/getGitChanges", controller.TaskWorkspace{}.GitChanges)
			taskWorkspaceGroup.POST("/getFileDiff", controller.TaskWorkspace{}.FileDiff)
		}

		/*
		 * workspace 模块：工作空间 CRUD（action 风格，POST）。
		 * 顶层资源（无 project 归属），独立分组，不挂 project 下。
		 */
		workspaceGroup := apiGroup.Group("/workspace")
		{
			workspaceGroup.POST("/getList", controller.Workspace{}.GetList)
			workspaceGroup.POST("/getInfo", controller.Workspace{}.GetInfo)
			workspaceGroup.POST("/create", controller.Workspace{}.Create)
			workspaceGroup.POST("/update", controller.Workspace{}.Update)
			workspaceGroup.POST("/delete", controller.Workspace{}.Delete)
		}

		// projectTask 模块：任务 CRUD（action 风格，POST），挂 project 下。
		projectTaskGroup := projectGroup.Group("/task")
		{
			projectTaskGroup.POST("/getList", controller.ProjectTask{}.GetList)
			projectTaskGroup.POST("/getInfo", controller.ProjectTask{}.GetInfo)
			projectTaskGroup.POST("/create", controller.ProjectTask{}.Create)
			projectTaskGroup.POST("/update", controller.ProjectTask{}.Update)
			projectTaskGroup.POST("/delete", controller.ProjectTask{}.Delete)
		}

		// type 模块：项目类型 CRUD（action 风格，POST），挂 project 下（类型随项目）。
		projectTypeGroup := projectGroup.Group("/type")
		{
			projectTypeGroup.POST("/getList", controller.ProjectType{}.GetList)
			projectTypeGroup.POST("/getInfo", controller.ProjectType{}.GetInfo)
			projectTypeGroup.POST("/create", controller.ProjectType{}.Create)
			projectTypeGroup.POST("/update", controller.ProjectType{}.Update)
			projectTypeGroup.POST("/delete", controller.ProjectType{}.Delete)
		}
	}

	/*
	 * MCP 端点（docs/agent_dev_01_tasks.md T2.1 + T4.1 合并定稿：单 server 归口，全部工具含 github_* 注册在 ocean_harness server）：Streamable HTTP 三方法——POST（JSON-RPC 工具调用）、GET（服务端 SSE 流）、DELETE（会话终止），SDK StreamableHTTPHandler 内部按 method 分支。
	 * T1.3 的 .mcp.json 写入 http://127.0.0.1:<port> + 本路径，变更须两端同步；无鉴权（本机单用户，与 /api 同口径），工具清单见 internal/mcpservers。
	 */
	mcpHandler := gin.WrapH(mcpservers.McpOceanHarnessStreamableHTTPHandler())
	r.Match([]string{"GET", "POST", "DELETE"}, "/mcp/streamableHttp/oceanHarness", mcpHandler)

	/*
	 * im 投递端点（bot 回合内文件投递，任务 8）：token 为路径尾段（即回合投递注册键），
	 * per-token server 见 mcpservers/mcp_im_delivery.go；驱动侧 --mcp-config 与本路径互为镜像。
	 */
	imDeliveryHandler := gin.WrapH(mcpservers.McpImDeliveryStreamableHTTPHandler())
	r.Match([]string{"GET", "POST", "DELETE"}, mcpservers.ImDeliveryPathPrefix+":token", imDeliveryHandler)

	return r
}
