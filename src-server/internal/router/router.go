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
// 当前仅暴露无需登录/鉴权的 /api/baseInfo/getServerRunInfo。
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

	baseInfoController := controller.NewBaseInfoController()

	// 路由按命名空间分组，便于后续扩展：/api/<module>/<action>。
	apiGroup := r.Group("/api")
	{
		baseInfoGroup := apiGroup.Group("/baseInfo")
		{
			baseInfoGroup.GET("/getServerRunInfo", baseInfoController.GetServerRunInfo)
		}
	}

	return r
}
