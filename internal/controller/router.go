/*
controller 包是表现层：gin handler 只做绑定、校验与响应，业务在 service。
*/
package controller

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

/* Controllers 是路由依赖集合（main 装配）。 */
type Controllers struct {
	Session  *SessionController
	Chat     *ChatController
	Settings *SettingsController
	Topics   *TopicController
}

/* NewRouter 装配 gin engine 与全部路由。dist 非 nil 时服务前端静态资源。 */
func NewRouter(c Controllers, dist fs.FS) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	api := r.Group("/api")
	{
		api.GET("/bootstrap", c.Session.Bootstrap)
		api.GET("/status", c.Session.Status)

		api.GET("/sessions/:id", c.Session.History)
		api.GET("/sessions/:id/events", c.Chat.Events)
		api.POST("/sessions/:id/messages", c.Chat.SendMessage)
		api.POST("/sessions/:id/cancel", c.Chat.CancelTurn)
		api.POST("/sessions/:id/summary", c.Session.Summary)

		api.GET("/settings", c.Settings.GetSettings)
		api.POST("/settings", c.Settings.UpdateSettings)
		api.GET("/memory", c.Settings.GetMemory)
		api.POST("/memory", c.Settings.SaveMemory)

		api.GET("/topics", c.Topics.List)
		api.GET("/topics/:id", c.Topics.Get)
		api.POST("/topics/:id/resume", c.Topics.Resume)

		api.POST("/sessions/:id/decisions/approve", c.Chat.DecideApprove)
		api.POST("/sessions/:id/decisions/answer", c.Chat.DecideAnswer)
		api.POST("/sessions/:id/decisions/plan", c.Chat.DecidePlan)
	}

	if dist != nil {
		serveStatic(r, dist)
	}
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
