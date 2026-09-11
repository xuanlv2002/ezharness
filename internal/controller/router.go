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
	Session   *SessionController
	Chat      *ChatController
	Settings  *SettingsController
	Topics    *TopicController
	Mcp       *McpController
	Apps      *AppsController
	App       *AppController
	Window    *WindowController
	Terminal  *TerminalController
	Workspace *WorkspaceController
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
		api.GET("/sessions/:id/prev", c.Session.Prev)
		api.GET("/sessions/:id/forks/:fid", c.Session.Fork)
		api.GET("/sessions/:id/events", c.Chat.Events)
		api.GET("/notifications", c.Chat.Notifications)
		api.POST("/sessions/:id/messages", c.Chat.SendMessage)
		api.POST("/sessions/:id/cancel", c.Chat.CancelTurn)
		api.POST("/sessions/:id/summary", c.Session.Summary)
		api.POST("/sessions/:id/fork", c.Topics.ForkBranch) // :id=源会话，body{anchor}复制前缀开新线

		api.GET("/settings", c.Settings.GetSettings)
		api.POST("/settings", c.Settings.UpdateSettings)
		api.GET("/models", c.Settings.GetModels)
		api.POST("/models", c.Settings.UpdateModels)
		api.GET("/security", c.Settings.GetSecurity)
		api.POST("/security", c.Settings.UpdateSecurity)
		api.GET("/memory", c.Settings.GetMemory)
		api.POST("/memory", c.Settings.SaveMemory)
		api.GET("/memory/config", c.Settings.GetMemoryConfig)
		api.POST("/memory/skills", c.Settings.CreateSkill)             // zip base64 新建技能
		api.DELETE("/memory/skills/:id", c.Settings.DeleteSkill)       // 删除技能目录
		api.POST("/memory/skills/:id/enabled", c.Settings.ToggleSkill) // 启停技能

		api.GET("/topics", c.Topics.List)
		api.GET("/topics/:id", c.Topics.Get)
		api.DELETE("/topics/:id", c.Topics.Delete)

		api.POST("/branches/new", c.Topics.NewBranch)
		api.POST("/branches/:id/activate", c.Topics.Activate)
		api.GET("/memory/tree", c.Topics.Tree)
		api.POST("/sessions/:id/archive", c.Topics.Archive) // 手动归档预留
		api.POST("/topics/compact", c.Topics.Compact)       // 归档换代：活动会话总结归档开新篇

		api.GET("/mcp", c.Mcp.List)
		api.POST("/mcp", c.Mcp.Update)
		api.POST("/mcp/connect", c.Mcp.Connect)
		api.POST("/mcp/disconnect", c.Mcp.Disconnect)
		api.POST("/mcp/call", c.Mcp.Call)

		api.GET("/apps", c.Apps.List)
		api.POST("/apps/open", c.Apps.Open)

		api.GET("/workspace/file", c.Workspace.File)         // 工作目录文件预览（附件 chips 源）
		api.HEAD("/workspace/file", c.Workspace.File)        // 同上（文件页轮询 Last-Modified 用；gin 不自动映射 HEAD→GET）
		api.POST("/workspace/save", c.Workspace.Save)        // 工作目录文本保存（资源查看器·文本）
		api.POST("/workspace/save-bin", c.Workspace.SaveBin) // 二进制写回（资源查看器·图片画板保存）
		api.POST("/workspace/stash", c.Workspace.Stash)      // 附件暂存（拖入即落盘 tmp/，发送只传路径引用）

		/* 魔法看板·共享终端：WS 多路复用 + REST 管理 */
		api.GET("/terminal/ws", c.Terminal.Ws)
		api.GET("/terminal/list", c.Terminal.List)
		api.POST("/terminal/create", c.Terminal.Create)
		api.POST("/terminal/close", c.Terminal.Close)

		api.POST("/window/min", c.Window.Minimise)
		api.POST("/window/max", c.Window.ToggleMaximise)
		api.GET("/window/state", c.Window.State)
		api.POST("/window/close", c.Window.Close)
		api.POST("/window/close-decision", c.Window.CloseDecision)
		api.POST("/window/open-url", c.Window.OpenURL)
		api.POST("/window/open-web", c.Window.OpenWeb)

		api.POST("/sessions/:id/decisions/approve", c.Chat.DecideApprove)
		api.POST("/sessions/:id/decisions/answer", c.Chat.DecideAnswer)
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
