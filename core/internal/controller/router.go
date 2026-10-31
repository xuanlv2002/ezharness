/*
controller 包是表现层：gin handler 只做绑定、校验与响应，业务在 service。
*/
package controller

import (
	"crypto/subtle"
	"io/fs"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

/* authCookieName 是浏览器自动请求的鉴权 cookie（HttpOnly、SameSite=Strict）。 */
const authCookieName = "ez_token"

/* openapiTokenHeader 是 OpenAPI 面的令牌请求头。 */
const openapiTokenHeader = "X-EZ-Open-Token"

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
TokenStore 持有两枚 API 令牌，重置/换代时原地更新、中间件实时读取：

  - session：前端（桌面壳+网页）会话令牌，每次启动换，只活在进程内
  - openapi：OpenAPI 面令牌，持久于数据目录 openapi.json，可重置换新
*/
type TokenStore struct {
	mu      sync.RWMutex
	session string
	openapi string
}

/* NewTokenStore 构造令牌组。 */
func NewTokenStore(session, openapi string) *TokenStore {
	return &TokenStore{session: session, openapi: openapi}
}

/* Session 返回会话令牌。 */
func (t *TokenStore) Session() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.session
}

/* Openapi 返回 OpenAPI 令牌。 */
func (t *TokenStore) Openapi() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.openapi
}

/* SetOpenapi 原地更新 OpenAPI 令牌（重置后中间件即时生效）。 */
func (t *TokenStore) SetOpenapi(v string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.openapi = v
}

/*
NewRouter 装配 gin engine 与全部路由。dist 非 nil 时服务前端静态资源。
路由分两面：

  - 会话面 /api/*：全量能力，会话凭证（X-EZ-Token / cookie / WS 子协议）
  - OpenAPI 面 /api/open/*：模型直调与 MCP 调用，openapi 令牌或会话凭证，
    跨源放行（file:// 页面、外部脚本直调）；SDK 免鉴权伺服
*/
func NewRouter(c Controllers, dist fs.FS, tokens *TokenStore) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	/* 换代探活：旧页面重启后在新 origin 上跨域轮询，必须免鉴权（只回 boot） */
	r.GET("/api/app/health", c.App.Health)

	/* OpenAPI SDK：本身无秘密，免鉴权、跨源放行，任何页面可直接引 */
	r.GET("/api/open/sdk.js", corsOpen(), serveSdk)

	/* CORS 预检：浏览器发 OPTIONS，gin 按 method 匹配路由——未注册的
	   method 直接 404、中间件不跑，预检必须显式挂通配路由应答。 */
	r.OPTIONS("/api/open/*path", corsOpen())

	open := r.Group("/api/open", corsOpen(), openAuth(tokens))
	{
		open.POST("/model/call", c.Settings.CallModel) // 直调大模型
		open.POST("/mcp/call", c.Mcp.Call)             // 调 MCP 工具
		open.GET("/mcp", c.Mcp.OpenList)               // 已启用服务清单（不含配置细节）
		/* MCP OAuth：SDK/脚本侧发起授权（授权本身由用户在浏览器完成） */
		open.POST("/mcp/oauth/start", c.Mcp.OAuthStart)
		open.GET("/mcp/oauth/status", c.Mcp.OAuthStatus)
		open.POST("/mcp/oauth/cancel", c.Mcp.OAuthCancel)
	}

	api := r.Group("/api", authRequired(tokens))
	{
		api.GET("/app/config", c.App.Config)
		api.POST("/app/restart", c.App.Restart)
		/* 令牌管理（设置页展示/复制/重置；能过鉴权者才可见） */
		api.GET("/app/token", c.App.SessionToken)
		api.GET("/app/openapi-token", c.App.OpenapiToken)
		api.POST("/app/openapi-token/reset", c.App.ResetOpenapiToken)
		/* 令牌换 HttpOnly cookie：浏览器自动发起的导航、图片/PDF 请求
		   无法加自定义头，前端启动时用入口令牌换一次 cookie。 */
		api.POST("/auth/cookie", c.App.AuthCookie)

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
		api.POST("/mcp/oauth/probe", c.Mcp.OAuthProbe)
		api.POST("/mcp/oauth/logout", c.Mcp.OAuthLogout)

		api.GET("/apps", c.Apps.List)
		api.POST("/apps/open", c.Apps.Open)     // 名单校验+返回路径标题；开窗由 desktop 壳/web 端执行
		api.POST("/apps/:name/pin", c.Apps.Pin) // 钉到桌面小助手/取消

		api.GET("/workspace/file", c.Workspace.File)    // 工作目录文件预览（附件 chips 源）
		api.HEAD("/workspace/file", c.Workspace.File)   // 同上（文件页轮询 Last-Modified 用；gin 不自动映射 HEAD→GET）
		api.POST("/workspace/save", c.Workspace.Save)   // 工作目录文本保存（资源查看器·文本）
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
	/* 快应用静态服务：apps/ 下的文件原样返回 */
	r.GET("/apps/*filepath", serveApps())
	return r
}

// authRequired 校验会话面令牌：X-EZ-Token 头 / ez_token cookie / WS 子协议
// 任一匹配即可（恒定时间比较）；不认来源（Origin/Host 可伪造）。
func authRequired(tokens *TokenStore) gin.HandlerFunc {
	return func(g *gin.Context) {
		if validToken(g, tokens.Session()) {
			g.Next()
			return
		}
		g.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}

/*
openAuth 鉴权 OpenAPI 面：X-EZ-Open-Token 命中 openapi 令牌，或持有效
会话凭证（ezharness 前端与同源页面）。同源页面 cookie 与页面自带令牌头
两通道并存、任一命中即过——硬编码令牌已被重置的页面，同源下凭 cookie
仍可调用。
*/
func openAuth(tokens *TokenStore) gin.HandlerFunc {
	return func(g *gin.Context) {
		if t := g.GetHeader(openapiTokenHeader); t != "" &&
			subtle.ConstantTimeCompare([]byte(t), []byte(tokens.Openapi())) == 1 {
			g.Next()
			return
		}
		if validToken(g, tokens.Session()) {
			g.Next()
			return
		}
		g.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}

/*
corsOpen 放行 OpenAPI 面跨源：file:// 页面与外部脚本直调 core。鉴权只认
令牌头，不放开凭证（ACAO * 与 cookie 凭证互斥；同源页面的 cookie 不受
跨源配置影响）。
*/
func corsOpen() gin.HandlerFunc {
	return func(g *gin.Context) {
		h := g.Writer.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, X-EZ-Open-Token, X-EZ-Token")
		h.Set("Access-Control-Max-Age", "86400")
		if g.Request.Method == http.MethodOptions {
			g.AbortWithStatus(http.StatusNoContent)
			return
		}
		g.Next()
	}
}

// validToken 收集会话凭证通道并逐一恒定时间比较。cookie 供浏览器自动
// 发起的请求（导航、<img>/<video>/PDF、快应用页）使用——这些请求加不了
// 自定义请求头。任一通道有效即通过。
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
