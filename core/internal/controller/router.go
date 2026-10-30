/*
controller 包是表现层：gin handler 只做绑定、校验与响应，业务在 service。
*/
package controller

import (
	"crypto/subtle"
	"io/fs"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

/* authCookieName 是浏览器自动请求的鉴权 cookie（HttpOnly、SameSite=Strict）。 */
const authCookieName = "ez_token"

/* Controllers 是路由依赖集合（main 装配）。 */
type Controllers struct {
	Session   *SessionController
	Chat      *ChatController
	Settings  *SettingsController
	Topics    *TopicController
	Mcp       *McpController
	Apps      *AppsController
	App       *AppController
	Terminal  *TerminalController
	Browser   *BrowserController
	Workspace *WorkspaceController
}

/*
NewRouter 装配 gin engine 与全部路由。dist 非 nil 时服务前端静态资源；
token 是 API 访问令牌（进程级常量，换代沿用）。
*/
func NewRouter(c Controllers, dist fs.FS, token string) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	/* 换代探活：旧页面重启后在新 origin 上跨域轮询，必须免鉴权（只回 boot） */
	r.GET("/api/app/health", c.App.Health)

	api := r.Group("/api", authRequired(token))
	{
		api.GET("/app/config", c.App.Config)
		api.POST("/app/restart", c.App.Restart)
		/* 当前访问令牌：设置页展示/复制用（能过鉴权者才可见）。 */
		api.GET("/app/token", func(g *gin.Context) {
			g.JSON(http.StatusOK, gin.H{"token": token})
		})
		/* 令牌换 HttpOnly cookie：浏览器自动发起的导航、图片/PDF、快应用
		   请求无法加自定义头，前端启动时用入口令牌换一次 cookie。 */
		api.POST("/auth/cookie", func(g *gin.Context) {
			setAuthCookie(g, token)
			g.Status(http.StatusNoContent)
		})

		api.GET("/bootstrap", c.Session.Bootstrap)
		api.GET("/status", c.Session.Status)
		api.GET("/stats/hourly", c.Session.HourlyStats)

		api.GET("/sessions/:id", c.Session.History)
		api.GET("/sessions/:id/prev", c.Session.Prev)
		api.GET("/sessions/:id/forks/:fid", c.Session.Fork)
		api.GET("/sessions/:id/events", c.Chat.Events)
		api.GET("/notifications", c.Chat.Notifications)
		api.POST("/sessions/:id/notifications/dismiss", c.Chat.DismissNotification)
		api.POST("/sessions/:id/messages", c.Chat.SendMessage)
		api.POST("/sessions/:id/cancel", c.Chat.CancelTurn)
		api.POST("/sessions/:id/fork", c.Topics.ForkBranch) // :id=源会话，body{anchor}复制前缀开新线

		api.GET("/settings", c.Settings.GetSettings)
		api.POST("/settings", c.Settings.UpdateSettings)
		api.POST("/settings/reasoning", c.Settings.SetReasoning)      // 思考档位（输入框下拉，即改即生效）
		api.POST("/settings/approve-mode", c.Settings.SetApproveMode) // 审批档位（输入框下拉，即改即生效）
		api.POST("/model/call", c.Settings.CallModel)                 // 直调大模型（快应用/脚本；与 /mcp/call 同级）
		api.GET("/models", c.Settings.GetModels)
		api.POST("/models", c.Settings.UpdateModels)
		api.POST("/models/activate", c.Settings.ActivateModel) // 主模型切换（输入框选择器；下一次输入生效）
		api.GET("/security", c.Settings.GetSecurity)
		api.POST("/security", c.Settings.UpdateSecurity)
		api.GET("/memory/config", c.Settings.GetMemoryConfig)
		api.GET("/memory/longterm", c.Settings.GetLongterm)                    // 条目式长期记忆全量
		api.POST("/memory/longterm/:target", c.Settings.SaveLongtermItems)     // user|soul 整组保存（记忆页编辑器）
		api.GET("/memory/project/:id", c.Settings.GetProject)                  // 项目记忆详情
		api.POST("/memory/project", c.Settings.SaveProject)                    // 新建/更新项目记忆
		api.DELETE("/memory/project/:id", c.Settings.DeleteProject)            // 删除项目
		api.POST("/memory/pending/:id/decide", c.Settings.DecideMemoryPending) // 待确认记忆操作：确认应用/取消丢弃
		api.POST("/memory/skills", c.Settings.CreateSkill)                     // zip base64 新建技能
		api.GET("/memory/skills/:id", c.Settings.GetSkill)                     // 技能详情（SKILL.md 正文）
		api.DELETE("/memory/skills/:id", c.Settings.DeleteSkill)               // 删除技能目录
		api.POST("/memory/skills/:id/enabled", c.Settings.ToggleSkill)         // 启停技能

		api.GET("/topics", c.Topics.List)
		api.GET("/topics/:id", c.Topics.Get)
		api.GET("/topics/:id/trace", c.Topics.Trace) // 回顾页调用链视图（otel span 列表）
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

		/* MCP OAuth 认证：前端取授权 URL 用系统浏览器打开，轮询状态 */
		api.POST("/mcp/oauth/start", c.Mcp.OAuthStart)
		api.POST("/mcp/oauth/probe", c.Mcp.OAuthProbe)
		api.GET("/mcp/oauth/status", c.Mcp.OAuthStatus)
		api.POST("/mcp/oauth/cancel", c.Mcp.OAuthCancel)
		api.POST("/mcp/oauth/logout", c.Mcp.OAuthLogout)

		api.GET("/apps", c.Apps.List)
		api.POST("/apps/open", c.Apps.Open)     // 名单校验+返回路径标题；开窗由 desktop 壳/web 端执行
		api.POST("/apps/:name/pin", c.Apps.Pin) // 钉到桌面小助手/取消

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

		/* 魔法看板·共享浏览器：desktop 壳桥（工具调用转发端点） */
		api.GET("/browser/bridge", c.Browser.Bridge)

		api.POST("/sessions/:id/decisions/approve", c.Chat.DecideApprove)
		api.POST("/sessions/:id/decisions/answer", c.Chat.DecideAnswer)
		api.POST("/sessions/:id/decisions/mcp-auth", c.Chat.DecideMcpAuth)
	}

	if dist != nil {
		serveStatic(r, dist)
	}
	/* 快应用静态服务：apps/ 下的 html 由前端直开（页面注入令牌桥） */
	r.GET("/apps/*filepath", serveApps())
	return r
}

// authRequired 校验 API 令牌：X-EZ-Token 头 / ez_token cookie / WS 子协议
// 任一匹配即可（恒定时间比较）；不认来源（Origin/Host 可伪造）。
func authRequired(token string) gin.HandlerFunc {
	return func(g *gin.Context) {
		if validToken(g, token) {
			g.Next()
			return
		}
		g.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}

// validToken 收集所有令牌通道并逐一恒定时间比较。cookie 供浏览器自动
// 发起的请求（导航、<img>/<video>/PDF、快应用页）使用——这些请求加不了
// 自定义请求头。任一通道有效即通过，避免残留的旧 cookie/旧头互相干扰。
func validToken(g *gin.Context, token string) bool {
	candidates := []string{g.GetHeader("X-EZ-Token")}
	if c, err := g.Cookie(authCookieName); err == nil {
		candidates = append(candidates, c)
	}
	if t, _ := wsProtocolToken(g.GetHeader("Sec-WebSocket-Protocol")); t != "" {
		candidates = append(candidates, t)
	}
	for _, c := range candidates {
		if c != "" && subtle.ConstantTimeCompare([]byte(c), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

// setAuthCookie 下发 HttpOnly 会话 cookie：SameSite=Strict 阻断跨站请求
// 顺手携带；HTTPS 下补 Secure。
func setAuthCookie(g *gin.Context, token string) {
	http.SetCookie(g.Writer, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   g.Request.TLS != nil,
	})
}

// wsProtocolToken 从 Sec-WebSocket-Protocol 提取前端令牌与完整子协议值：
// 浏览器 WebSocket 不能设置自定义请求头，前端把令牌编码成
// `ez-token.<token>` 子协议。Accept 需要把这个值原样回显，否则部分客户端
// （如 ws）会以 "Server sent no subprotocol" 拒绝连接。
func wsProtocolToken(header string) (token, proto string) {
	for _, p := range strings.Split(header, ",") {
		p = strings.TrimSpace(p)
		if t, ok := strings.CutPrefix(p, "ez-token."); ok {
			return t, p
		}
	}
	return "", ""
}

// wsAcceptOptions 组装 WebSocket Accept 选项：放行本机开发来源；若客户端
// 用子协议携带令牌，回显该子协议（ws 等客户端要求服务端确认所选协议）。
func wsAcceptOptions(g *gin.Context) *websocket.AcceptOptions {
	opts := &websocket.AcceptOptions{OriginPatterns: []string{"localhost:*", "127.0.0.1:*"}}
	if _, proto := wsProtocolToken(g.GetHeader("Sec-WebSocket-Protocol")); proto != "" {
		opts.Subprotocols = []string{proto}
	}
	return opts
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
		/* assets 是构建产物：缺失即 404。回退 index.html 会让模块请求拿到
		   text/html，动态 import 只报模糊的 "Failed to fetch" */
		if strings.HasPrefix(path, "assets/") {
			g.Status(http.StatusNotFound)
			return
		}
		g.Request.URL.Path = "/"
		fileServer.ServeHTTP(g.Writer, g.Request)
	})
}
