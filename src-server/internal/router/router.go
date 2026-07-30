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

			// projectState 模块：状态 CRUD + reorder（action 风格，POST）。
			projectStateGroup := trackerGroup.Group("/projectState")
			{
				projectStateGroup.POST("/getList", controller.ProjectState{}.GetList)
				projectStateGroup.POST("/create", controller.ProjectState{}.Create)
				projectStateGroup.POST("/update", controller.ProjectState{}.Update)
				projectStateGroup.POST("/delete", controller.ProjectState{}.Delete)
				projectStateGroup.POST("/reorder", controller.ProjectState{}.Reorder)
			}

			// project 模块：项目 CRUD（action 风格，POST）。
			projectGroup := trackerGroup.Group("/project")
			{
				projectGroup.POST("/getList", controller.Project{}.GetList)
				projectGroup.POST("/getInfo", controller.Project{}.GetInfo)
				projectGroup.POST("/create", controller.Project{}.Create)
				projectGroup.POST("/update", controller.Project{}.Update)
				projectGroup.POST("/delete", controller.Project{}.Delete)
			}

			// workspaceLabel 模块：标签 CRUD + toggleIssue（action 风格，POST）。
			workspaceLabelGroup := trackerGroup.Group("/workspaceLabel")
			{
				workspaceLabelGroup.POST("/getList", controller.WorkspaceLabel{}.GetList)
				workspaceLabelGroup.POST("/getInfo", controller.WorkspaceLabel{}.GetInfo)
				workspaceLabelGroup.POST("/create", controller.WorkspaceLabel{}.Create)
				workspaceLabelGroup.POST("/update", controller.WorkspaceLabel{}.Update)
				workspaceLabelGroup.POST("/delete", controller.WorkspaceLabel{}.Delete)
				workspaceLabelGroup.POST("/toggleIssue", controller.WorkspaceLabel{}.ToggleIssue)
			}
		}
	}

	return r
}
