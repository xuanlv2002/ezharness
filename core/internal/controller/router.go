/*
controller 包是表现层：gin handler 只做绑定、校验与响应，业务在 service。
路由分两面：会话面 /api/*（EzHarnessToken，用户界面）与 OpenAPI 面
/api/open/*（EzHarnessOpenToken，agent 产物）。两面认证完全独立，见
auth.go 与 openapi.go。
*/
package controller

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/service"
)

/* Controllers 是路由依赖集合（main 装配）。 */
type Controllers struct {
	Auth      *AuthController
	Openapi   *OpenapiController
	Session   *SessionController
	Chat      *ChatController
	Settings  *SettingsController
	Topics    *TopicController
	Mcp       *McpController
	Apps      *AppsController
	Callbacks *CallbackController
	App       *AppController
	Terminal  *TerminalController
	Browser   *BrowserController
	Workspace *WorkspaceController
	Pinboard  *PinboardController
	Media     *MediaController
}

/*
accessLog 取代 gin.Logger：轮询读的成功请求不落盘——悬浮球 3s 三点
（topics/notifications/apps）、回调页 5s 两点、资源树 3s，全量记一天
8 万+ 行，把 core-*.log 淹得没法看。这些端点失败（>=400）照记，其余
请求全记。格式沿用 gin 默认行（去色，含 IP 与耗时）。
*/
func accessLog() gin.HandlerFunc {
	quiet := map[string]bool{
		"/api/app/health":     true, // 桌面壳启动探活 / 换代探活
		"/api/topics":         true, // 悬浮球运行态轮询
		"/api/notifications":  true, // 通知轮询（悬浮球/通知窗/主端）
		"/api/apps":           true, // 快应用清单轮询（悬浮球/回调页）
		"/api/callbacks":      true, // 回调页任务清单轮询
		"/api/workspace/tree": true, // 资源页目录树轮询（AI 改盘跟进）
	}
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if c.Request.Method == http.MethodGet &&
			c.Writer.Status() < http.StatusBadRequest && quiet[c.Request.URL.Path] {
			return
		}
		fmt.Fprintf(gin.DefaultWriter, "[GIN] %v | %3d | %13v | %15s | %-7s %s\n",
			start.Format("2006/01/02 - 15:04:05"), c.Writer.Status(),
			time.Since(start), c.ClientIP(), c.Request.Method, c.Request.URL.RequestURI())
	}
}

/*
NewRouter 装配 gin engine 与全部路由。dist 非 nil 时服务前端静态资源。
免鉴权面全量清单：health、/api/auth/login、/api/open/sdk.js、/apps/*
静态资产——其余全部要凭证。
*/
func NewRouter(c Controllers, dist fs.FS, auth *service.AuthStore, openapi *service.OpenapiStore) *gin.Engine {
	r := gin.New()
	r.Use(accessLog(), gin.Recovery())

	/* 换代探活：旧页面重启后在新 origin 上跨域轮询，必须免鉴权（只回 boot/version） */
	r.GET("/api/app/health", c.App.Health)

	/* 认证：默认密码换会话令牌（三端启动时静默调用） */
	r.POST("/api/auth/login", c.Auth.Login)

	/* OpenAPI SDK：本身无秘密，免鉴权、跨源放行，任何页面可直接引 */
	r.GET("/api/open/sdk.js", corsOpen(), serveSdk)

	/* CORS 预检：浏览器发 OPTIONS，gin 按 method 匹配路由——未注册的
	   method 直接 404、中间件不跑，预检必须显式挂通配路由应答。 */
	r.OPTIONS("/api/open/*path", corsOpen())

	open := r.Group("/api/open", corsOpen(), openGuard(openapi))
	{
		open.POST("/model/call", c.Settings.CallModel) // 直调大模型
		open.POST("/mcp/call", c.Mcp.Call)             // 调 MCP 工具
		open.GET("/mcp", c.Mcp.OpenList)               // 已启用服务清单（不含配置细节）
		/* 多媒体生成（异步任务：提交短等待，未完成持 id 轮询；贵价：每日配额统一在 MediaService，超限 429） */
		open.POST("/image/call", c.Media.ImageCall) // 文生图
		open.POST("/audio/call", c.Media.AudioCall) // 语音合成
		open.POST("/video/call", c.Media.VideoCall) // 视频生成（数分钟，wait 后查 jobs）
		open.GET("/media/jobs", c.Media.JobsGET)    // 最近任务清单
		open.GET("/media/jobs/:id", c.Media.JobGET) // 任务详情（done 带产物）
		/* MCP OAuth：SDK/脚本侧发起授权（授权本身由用户在浏览器完成） */
		open.POST("/mcp/oauth/start", c.Mcp.OAuthStart)
		open.GET("/mcp/oauth/status", c.Mcp.OAuthStatus)
		open.POST("/mcp/oauth/cancel", c.Mcp.OAuthCancel)
	}

	api := r.Group("/api", sessionAuth(auth))
	{
		api.POST("/auth/logout", c.Auth.Logout)
		api.GET("/auth/sessions", c.Auth.Sessions)
		api.DELETE("/auth/sessions/:id", c.Auth.RevokeSession)

		api.GET("/app/config", c.App.Config)
		api.GET("/app/update-check", c.App.UpdateCheck) // 版本检查：最新 release 摘要（比对在前端）
		api.POST("/app/restart", c.App.Restart)
		api.POST("/app/format", c.App.Format) // 格式化：清空全部配置/会话/数据并重建（不可恢复）

		/* OpenAPI 令牌管理（安全页，属于用户会话面） */
		api.GET("/security/openapi-tokens", c.Openapi.Tokens)
		api.POST("/security/openapi-tokens", c.Openapi.Create)
		api.DELETE("/security/openapi-tokens/:id", c.Openapi.Revoke)
		api.GET("/security/openapi-audit", c.Openapi.Audit)

		api.GET("/bootstrap", c.Session.Bootstrap)
		api.GET("/status", c.Session.Status)
		api.GET("/stats/hourly", c.Session.HourlyStats)

		api.GET("/sessions/:id", c.Session.History)
		api.POST("/sessions/:id/prefs", c.Session.SetPrefs) // 本会话模型/思考/审批偏好
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
		api.GET("/models", c.Settings.GetModels)
		api.POST("/models", c.Settings.UpdateModels)
		api.GET("/media", c.Media.ConfigGET)      // 多媒体生成模型配置（多媒体页数据源）
		api.POST("/media", c.Media.ConfigPUT)     // 保存并重建 agent（busy 拒绝）
		api.GET("/media/quota", c.Media.QuotaGET) // 三类生成任务当日用量/限额（工作台配额条）
		/* 多媒体调用与任务：界面走会话面；外部脚本走 /api/open/* 的同一 handler */
		api.POST("/media/image/call", c.Media.ImageCall)
		api.POST("/media/audio/call", c.Media.AudioCall)
		api.POST("/media/video/call", c.Media.VideoCall)
		api.GET("/media/jobs", c.Media.JobsGET)
		api.GET("/media/jobs/:id", c.Media.JobGET)
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
		api.POST("/mcp/call", c.Mcp.Call) // 界面调 MCP（外部脚本走 /api/open/mcp/call）
		api.POST("/mcp/server", c.Mcp.SaveServer)
		api.DELETE("/mcp/server/:name", c.Mcp.DeleteServer)
		api.POST("/mcp/connect", c.Mcp.Connect)
		api.POST("/mcp/disconnect", c.Mcp.Disconnect)
		api.POST("/mcp/oauth/probe", c.Mcp.OAuthProbe)
		api.POST("/mcp/oauth/start", c.Mcp.OAuthStart)   // 界面发起授权；open 面同一 handler
		api.GET("/mcp/oauth/status", c.Mcp.OAuthStatus)  // 界面轮询授权状态
		api.POST("/mcp/oauth/cancel", c.Mcp.OAuthCancel) // 界面取消授权
		api.POST("/mcp/oauth/logout", c.Mcp.OAuthLogout)

		api.GET("/apps", c.Apps.List)
		api.POST("/apps/open", c.Apps.Open)         // 名单校验+返回路径标题；开窗由 desktop 壳/web 端执行
		api.POST("/apps/:name/pin", c.Apps.Pin)     // 钉到桌面小助手/取消
		api.DELETE("/apps/:name", c.Apps.Uninstall) // 卸载：停后端终端+删目录+清钉选

		/* 定时回调中心：会话面控制面，不进 OpenAPI */
		api.GET("/callbacks", c.Callbacks.List)
		api.POST("/callbacks", c.Callbacks.Create)
		api.POST("/callbacks/:id", c.Callbacks.Update)
		api.DELETE("/callbacks/:id", c.Callbacks.Delete)
		api.POST("/callbacks/:id/enabled", c.Callbacks.SetEnabled)
		api.POST("/callbacks/:id/run", c.Callbacks.Run)

		api.GET("/workspace/file", c.Workspace.File)         // 工作目录文件预览（附件 chips 源）
		api.HEAD("/workspace/file", c.Workspace.File)        // 同上（文件页轮询 Last-Modified 用；gin 不自动映射 HEAD→GET）
		api.GET("/workspace/meta", c.Workspace.Meta)         // 附件卡轻量元数据（大小 / pptx 页数）
		api.POST("/workspace/save", c.Workspace.Save)        // 工作目录文本保存（资源查看器·文本）
		api.POST("/workspace/create", c.Workspace.Create)    // 目录树新建（文件/文件夹）
		api.POST("/workspace/save-bin", c.Workspace.SaveBin) // 二进制写回（资源查看器·图片画板保存）
		api.POST("/workspace/stash", c.Workspace.Stash)      // 附件暂存（拖入即落盘 tmp/，发送只传路径引用）
		api.GET("/workspace/workdir", c.Workspace.WorkDir)   // 解析后的工作目录（目录树默认根）
		api.GET("/workspace/tree", c.Workspace.Tree)         // 目录树一层 + git 工作区变更（资源页目录树）
		api.GET("/workspace/git-blob", c.Workspace.GitBlob)  // 文件 HEAD 原文（变更对比左侧）

		/* 便签板：整块板 JSON 原样读写（布局/交互在前端）；图片单独落盘，
		   板 JSON 只存 URL（<img> 走 cookie 鉴权） */
		api.GET("/pinboard", c.Pinboard.Get)
		api.POST("/pinboard", c.Pinboard.Save)
		api.POST("/pinboard/images", c.Pinboard.SaveImage)
		api.GET("/pinboard/images/:name", c.Pinboard.Image)

		/* 魔法看板·共享终端：WS 多路复用 + REST 管理 */
		api.GET("/terminal/ws", c.Terminal.Ws)
		api.GET("/terminal/list", c.Terminal.List)
		api.POST("/terminal/create", c.Terminal.Create)
		api.POST("/terminal/close", c.Terminal.Close)

		/* 魔法看板·共享浏览器：desktop 壳桥（工具调用转发端点） */
		api.GET("/browser/bridge", c.Browser.Bridge)

		api.POST("/sessions/:id/decisions/approve", c.Chat.DecideApprove)
		api.POST("/sessions/:id/decisions/answer", c.Chat.DecideAnswer)
		api.POST("/sessions/:id/decisions/plan", c.Chat.DecidePlan) // 计划确认（confirm/feedback）
		api.POST("/sessions/:id/decisions/mcp-auth", c.Chat.DecideMcpAuth)
	}

	if dist != nil {
		serveStatic(r, dist)
	}
	/* 快应用静态服务：apps/ 下的文件原样返回 */
	r.GET("/apps/*filepath", serveApps())
	return r
}

/*
corsOpen 放行 OpenAPI 面跨源：file:// 页面与外部脚本直调 core。只认
EzHarnessOpenToken，不涉及任何会话凭证或 cookie。
*/
func corsOpen() gin.HandlerFunc {
	return func(g *gin.Context) {
		h := g.Writer.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, "+openapiTokenHeader)
		h.Set("Access-Control-Max-Age", "86400")
		if g.Request.Method == http.MethodOptions {
			g.AbortWithStatus(http.StatusNoContent)
			return
		}
		g.Next()
	}
}

/* serveStatic 服务 SPA：静态资源 + 非 /api 路径回退 index.html。 */
func serveStatic(r *gin.Engine, dist fs.FS) {
	fileServer := http.FileServerFS(dist)
	r.NoRoute(func(g *gin.Context) {
		/* API 路径不存在就是 404：回退 index.html 会把 200+HTML 塞给调用方，
		   报错退化成 "Unexpected token '<'" 这类迷惑形态 */
		if strings.HasPrefix(g.Request.URL.Path, "/api/") {
			g.Status(http.StatusNotFound)
			return
		}
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
