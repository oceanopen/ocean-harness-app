// Package router 组装 gin engine：中间件 + 路由。
package router

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"we-claude-terminal/go-server/internal/controller"
	"we-claude-terminal/go-server/internal/middleware"
)

// SetupRouter 构造 gin engine：注册中间件与路由。
//
// 当前暴露 /api/baseInfo（系统信息）与 /api/tracker/*（tracker 业务域：workspace 等），均无需登录/鉴权。
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

	// 路由按命名空间分组：/api/baseInfo（系统信息）；/api/tracker/<module>/<action>（工作区/项目/issue 等管理域）。
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

			// projectState 模块：getList（项目状态数据）+ catalog（固定状态目录，GET）。
			// 状态管理无独立 CRUD/reorder 接口——随项目 create/update 全量提交。
			projectStateGroup := trackerGroup.Group("/projectState")
			{
				projectStateGroup.GET("/catalog", controller.ProjectState{}.Catalog)
				projectStateGroup.POST("/getList", controller.ProjectState{}.GetList)
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

			// workspaceLabel 模块：标签 CRUD（action 风格，POST）。
			workspaceLabelGroup := trackerGroup.Group("/workspaceLabel")
			{
				workspaceLabelGroup.POST("/getList", controller.WorkspaceLabel{}.GetList)
				workspaceLabelGroup.POST("/getInfo", controller.WorkspaceLabel{}.GetInfo)
				workspaceLabelGroup.POST("/create", controller.WorkspaceLabel{}.Create)
				workspaceLabelGroup.POST("/update", controller.WorkspaceLabel{}.Update)
				workspaceLabelGroup.POST("/delete", controller.WorkspaceLabel{}.Delete)
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
				projectIssueGroup.POST("/updateState", controller.ProjectIssue{}.UpdateState)
			}

			// issueWorktree 模块：issue 开发流程 worktree 元数据（createWorktree/removeWorktree/getList，action 风格，POST）。
			// P1 桩（Module G）：createWorktree 写假路径记录、getList 真查作 worktreePath/worktreeId SSOT；真实现见 worktree_term.md §6。
			issueWorktreeGroup := trackerGroup.Group("/issueWorktree")
			{
				issueWorktreeGroup.POST("/createWorktree", controller.IssueWorktree{}.CreateWorktree)
				issueWorktreeGroup.POST("/removeWorktree", controller.IssueWorktree{}.RemoveWorktree)
				issueWorktreeGroup.POST("/getList", controller.IssueWorktree{}.GetList)
				issueWorktreeGroup.POST("/updateWorktree", controller.IssueWorktree{}.UpdateWorktree)
			}
		}
	}

	return r
}
