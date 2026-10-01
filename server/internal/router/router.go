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

// SetupRouter 构造 gin engine：注册中间件与路由。
//
// 当前暴露 /api/baseInfo（系统信息）、/api/localRepository/*（本地仓库）、/api/issueWorkspace/*
// （issue 运行工作空间初始化）、/api/pluginMarketplace/*（插件市场）与 /api/plugin/*
// （插件安装操作）、/api/agentCatalog/*（ACP agent 目录投影）、/api/tracker/*
// （tracker 业务域：workspace 等）与 /mcp/streamableHttp/*（MCP 端点，供工作空间内
// AI agent 调用），均无需登录/鉴权。
// gin.SetMode 已在 config.MustLoad 中按环境变量完成。
func SetupRouter() *gin.Engine {
	r := gin.New()

	// 中间件：请求访问日志 + panic 恢复 + CORS（webview origin 与 127.0.0.1 不同源）。
	// AccessLog 须在 Recovery 之前：handler panic 被 Recovery 兜住返回 500 后，访问日志仍能记录完整 status 与 latency。
	r.Use(middleware.AccessLog())
	r.Use(middleware.Recovery())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: false, // 与 AllowOrigins:* 配合须为 false
		MaxAge:           12 * time.Hour,
	}))

	// 路由按命名空间分组：/api/baseInfo（系统信息）；/api/localRepository、/api/issueWorkspace（独立顶层域）；
	// /api/tracker/<module>/<action>（工作区/项目/issue 等管理域）。
	apiGroup := r.Group("/api")
	{
		baseInfoGroup := apiGroup.Group("/baseInfo")
		{
			baseInfoGroup.GET("/getServerRunInfo", controller.BaseInfo{}.GetServerRunInfo)
		}

		// localRepository 模块：本地仓库 CRUD + git 刷新（action 风格，POST）。
		// 本地仓库为顶层资源（无 workspace 归属），独立分组，不挂 tracker 下。
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
		}

		// issueWorkspace 模块：issue 运行工作空间初始化（init 异步受理 + status 轮询读状态文件）
		// 与归档/取消（archive 删目录 + 流转 issue 状态，T3.2）、文件浏览（getFileTree/
		// getFileContent + fileRaw 图片字节直连，T5.1 本期列表 + 预览）、Git 变更
		//（getGitChanges/getFileDiff 未提交变更列表与内容对，T5.1「Git 变更」模式）。
		// 与 /api/tracker/workspace（任务管理容器）是两个概念，故独立顶层分组。
		issueWorkspaceGroup := apiGroup.Group("/issueWorkspace")
		{
			issueWorkspaceGroup.POST("/init", controller.IssueWorkspace{}.Init)
			issueWorkspaceGroup.POST("/status", controller.IssueWorkspace{}.Status)
			issueWorkspaceGroup.POST("/archive", controller.IssueWorkspace{}.Archive)
			issueWorkspaceGroup.POST("/getFileTree", controller.IssueWorkspace{}.FileTree)
			issueWorkspaceGroup.POST("/getFileContent", controller.IssueWorkspace{}.FileContent)
			issueWorkspaceGroup.GET("/fileRaw", controller.IssueWorkspace{}.FileRaw)
			issueWorkspaceGroup.POST("/getGitChanges", controller.IssueWorkspace{}.GitChanges)
			issueWorkspaceGroup.POST("/getFileDiff", controller.IssueWorkspace{}.FileDiff)
		}

		// pluginMarketplace 模块：插件市场投影与注册表操作（action 风格，POST）。
		// 无本地表：市场注册表与安装状态的 SSOT 恒为 claude 侧，实时投影（调 claude plugin CLI
		// + 扫描市场清单）。与 localRepository 同为顶层资源，独立分组，不挂 tracker 下。
		pluginMarketplaceGroup := apiGroup.Group("/pluginMarketplace")
		{
			pluginMarketplaceGroup.POST("/getList", controller.PluginMarketplace{}.GetList)
			pluginMarketplaceGroup.POST("/add", controller.PluginMarketplace{}.Add)
			pluginMarketplaceGroup.POST("/update", controller.PluginMarketplace{}.Update)
			pluginMarketplaceGroup.POST("/remove", controller.PluginMarketplace{}.Remove)
		}

		// agentCatalog 模块：ACP agent 目录只读投影（action 风格，POST）。
		// 无本地表：SSOT 为 sidecar 内嵌离线快照（internal/agentcatalog）；
		// 前端下拉可选面 = enabled 条目（launch_settings.agentCode 值域 SSOT，T1.2）。
		agentCatalogGroup := apiGroup.Group("/agentCatalog")
		{
			agentCatalogGroup.POST("/getList", controller.AgentCatalog{}.GetList)
		}

		// plugin 模块：按开发工具（cli）维度的插件安装操作（install/uninstall/enable/disable/
		// update，action 风格 POST）。cli 字段为多 CLI 扩展口子（v1 仅 claude），写操作统一
		// 返回最新列表投影，前端一次往返即完成刷新。
		pluginGroup := apiGroup.Group("/plugin")
		{
			pluginGroup.POST("/install", controller.Plugin{}.Install)
			pluginGroup.POST("/uninstall", controller.Plugin{}.Uninstall)
			pluginGroup.POST("/enable", controller.Plugin{}.Enable)
			pluginGroup.POST("/disable", controller.Plugin{}.Disable)
			pluginGroup.POST("/update", controller.Plugin{}.Update)
		}

		// imBot 模块：IM 渠道数字人 bot 管理（action 风格，POST）。渠道无关命名（channel 差异
		// 在 credential JSON 内）；写操作落库后经 supervisor 热更新连接，getList 合并运行态。
		imBotGroup := apiGroup.Group("/imBot")
		{
			imBotGroup.POST("/getList", controller.ImBot{}.GetList)
			imBotGroup.POST("/getInfo", controller.ImBot{}.GetInfo)
			imBotGroup.POST("/create", controller.ImBot{}.Create)
			imBotGroup.POST("/update", controller.ImBot{}.Update)
			imBotGroup.POST("/delete", controller.ImBot{}.Delete)
			imBotGroup.POST("/restart", controller.ImBot{}.Restart)
			imBotGroup.POST("/provisionBegin", controller.ImBot{}.ProvisionBegin)
			imBotGroup.POST("/provisionPoll", controller.ImBot{}.ProvisionPoll)
			imBotGroup.POST("/provisionCancel", controller.ImBot{}.ProvisionCancel)
		}

		trackerGroup := apiGroup.Group("/tracker")
		{
			// workspace 模块：一律 POST（action 风格 getList/getInfo/create/update/delete）。
			workspaceGroup := trackerGroup.Group("/workspace")
			{
				workspaceGroup.POST("/getList", controller.Workspace{}.GetList)
				workspaceGroup.POST("/getInfo", controller.Workspace{}.GetInfo)
				workspaceGroup.POST("/create", controller.Workspace{}.Create)
				workspaceGroup.POST("/update", controller.Workspace{}.Update)
				workspaceGroup.POST("/delete", controller.Workspace{}.Delete)
			}

			// project 模块：项目 CRUD（action 风格，POST）。
			projectGroup := trackerGroup.Group("/project")
			{
				projectGroup.POST("/getList", controller.Project{}.GetList)
				projectGroup.POST("/getInfo", controller.Project{}.GetInfo)
				projectGroup.POST("/create", controller.Project{}.Create)
				projectGroup.POST("/update", controller.Project{}.Update)
				projectGroup.POST("/delete", controller.Project{}.Delete)
				// 项目 ↔ 本地仓库 关联随 create/update 全量保存，ids 随项目响应返回（无独立读接口）。
			}

			// workspaceType 模块：类型 CRUD（action 风格，POST）。
			workspaceTypeGroup := trackerGroup.Group("/workspaceType")
			{
				workspaceTypeGroup.POST("/getList", controller.WorkspaceType{}.GetList)
				workspaceTypeGroup.POST("/getInfo", controller.WorkspaceType{}.GetInfo)
				workspaceTypeGroup.POST("/create", controller.WorkspaceType{}.Create)
				workspaceTypeGroup.POST("/update", controller.WorkspaceType{}.Update)
				workspaceTypeGroup.POST("/delete", controller.WorkspaceType{}.Delete)
			}

			// projectIssue 模块：issue CRUD + move（看板拖拽，action 风格，POST）。
			projectIssueGroup := trackerGroup.Group("/projectIssue")
			{
				projectIssueGroup.POST("/getList", controller.ProjectIssue{}.GetList)
				projectIssueGroup.POST("/getInfo", controller.ProjectIssue{}.GetInfo)
				projectIssueGroup.POST("/create", controller.ProjectIssue{}.Create)
				projectIssueGroup.POST("/update", controller.ProjectIssue{}.Update)
				projectIssueGroup.POST("/delete", controller.ProjectIssue{}.Delete)
				projectIssueGroup.POST("/move", controller.ProjectIssue{}.Move)
			}
		}
	}

	// MCP 端点（docs/agent_dev_01_tasks.md T2.1 + T4.1 合并定稿：单 server 归口，全部工具
	// 含 github_* 注册在 ocean_harness server）：Streamable HTTP 三方法——POST（JSON-RPC
	// 工具调用）、GET（服务端 SSE 流）、DELETE（会话终止），SDK StreamableHTTPHandler 内部
	// 按 method 分支。T1.3 的 .mcp.json 写入 http://127.0.0.1:<port> + 本路径，变更须两端
	// 同步。无鉴权（本机单用户，与 /api 同口径），工具清单见 internal/mcpservers。
	mcpHandler := gin.WrapH(mcpservers.McpOceanHarnessStreamableHTTPHandler())
	r.Match([]string{"GET", "POST", "DELETE"}, "/mcp/streamableHttp/oceanHarness", mcpHandler)

	return r
}
