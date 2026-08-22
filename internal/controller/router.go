/*
controller 包是表现层：gin handler 只做绑定、校验与响应，业务在 service。
*/
package controller

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ezharness/internal/service"
)

/* Controllers 是路由依赖集合（main 装配）。 */
type Controllers struct {
	Session  *SessionController
	Chat     *ChatController
	Settings *SettingsController
	Topics   *TopicController
	Mcp      *McpController
	Apps     *AppsController
	App      *AppController
}

/* NewRouter 装配 gin engine 与全部路由。dist 非 nil 时服务前端静态资源。 */
func NewRouter(c Controllers, dist fs.FS) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	api := r.Group("/api")
	{
		api.GET("/app/health", c.App.Health)
		api.GET("/app/config", c.App.Config)
		api.POST("/app/restart", c.App.Restart)

		api.GET("/bootstrap", c.Session.Bootstrap)
		api.GET("/status", c.Session.Status)

		api.GET("/sessions/:id", c.Session.History)
		api.GET("/sessions/:id/events", c.Chat.Events)
		api.POST("/sessions/:id/messages", c.Chat.SendMessage)
		api.POST("/sessions/:id/cancel", c.Chat.CancelTurn)
		api.POST("/sessions/:id/summary", c.Session.Summary)

		api.GET("/settings", c.Settings.GetSettings)
		api.POST("/settings", c.Settings.UpdateSettings)
		api.GET("/models", c.Settings.GetModels)
		api.POST("/models", c.Settings.UpdateModels)
		api.GET("/security", c.Settings.GetSecurity)
		api.POST("/security", c.Settings.UpdateSecurity)
		api.GET("/memory", c.Settings.GetMemory)
		api.POST("/memory", c.Settings.SaveMemory)
		api.GET("/memory/config", c.Settings.GetMemoryConfig)

		api.GET("/topics", c.Topics.List)
		api.GET("/topics/:id", c.Topics.Get)
		api.DELETE("/topics/:id", c.Topics.Delete)
		api.POST("/topics/:id/resume", c.Topics.Resume)

		api.GET("/mcp", c.Mcp.List)
		api.POST("/mcp", c.Mcp.Update)

		api.GET("/apps", c.Apps.List)

		api.POST("/sessions/:id/decisions/approve", c.Chat.DecideApprove)
		api.POST("/sessions/:id/decisions/answer", c.Chat.DecideAnswer)
		api.POST("/sessions/:id/decisions/plan", c.Chat.DecidePlan)
	}

	if dist != nil {
		serveStatic(r, dist)
	}
	/* 快应用静态服务：apps/ 下的 html 由前端直开 */
	appsFS := http.Dir(service.AppsDir)
	r.GET("/apps/*filepath", func(g *gin.Context) {
		g.Request.URL.Path = g.Param("filepath")
		http.FileServer(appsFS).ServeHTTP(g.Writer, g.Request)
	})
	return r
}

/* serveStatic 服务 SPA：静态资源 + 非 /api 路径回退 index.html。 */
func serveStatic(r *gin.Engine, dist fs.FS) {
	fileServer := http.FileServerFS(dist)
	r.NoRoute(func(g *gin.Context) {
		path := strings.TrimPrefix(g.Request.URL.Path, "/")
		if path != "" && fs.ValidPath(path) {
			if _, err := fs.Stat(dist, path); err == nil {
				fileServer.ServeHTTP(g.Writer, g.Request)
				return
			}
		}
		g.Request.URL.Path = "/"
		fileServer.ServeHTTP(g.Writer, g.Request)
	})
}
