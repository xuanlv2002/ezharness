/*
认证：两枚令牌、两个信任域，全部显式请求头——会话令牌 EzHarnessToken
（默认密码 login 签发，desktop/cli/浏览器页面用），OpenAPI 令牌
EzHarnessOpenToken（安全页生成，agent 产物用）。cookie 是同一会话凭证
的浏览器传输适配：img/video/PDF 等自动请求加不了自定义头。WS 握手用
子协议 ["agent-auth", token] 带令牌。/api/open/* 全量审计。
*/
package controller

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"ezharness/core/internal/service"
)

const (
	sessionTokenHeader = "EzHarnessToken"
	openapiTokenHeader = "EzHarnessOpenToken"
	authCookieName     = "ez_token"
	wsAuthProto        = "agent-auth"
)

/* AuthController 是认证与凭证管理端点（安全页数据面）。 */
type AuthController struct {
	Auth    *service.AuthStore
	Openapi *service.OpenapiStore
}

/* Login POST /api/auth/login：默认密码换会话令牌（三端启动时静默调用）。 */
func (c *AuthController) Login(g *gin.Context) {
	if !localOrigin(g) {
		g.JSON(http.StatusForbidden, gin.H{"error": "仅允许本机页面登录"})
		return
	}
	var req struct {
		Password string `json:"password"`
		Client   string `json:"client"`
	}
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s, err := c.Auth.Login(req.Password, req.Client, clientIP(g))
	if err != nil {
		g.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	setAuthCookie(g, s.Token)
	g.JSON(http.StatusOK, gin.H{"token": s.Token})
}

/* Logout POST /api/auth/logout：撤销当前令牌对应的会话。 */
func (c *AuthController) Logout(g *gin.Context) {
	c.Auth.Logout(currentToken(g))
	g.Status(http.StatusNoContent)
}

/* Sessions GET /api/auth/sessions：会话清单（不含 token 值）。 */
func (c *AuthController) Sessions(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"sessions": c.Auth.SessionList()})
}

/* RevokeSession DELETE /api/auth/sessions/:id：吊销指定会话。 */
func (c *AuthController) RevokeSession(g *gin.Context) {
	if err := c.Auth.RevokeSession(g.Param("id")); err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	g.Status(http.StatusNoContent)
}

/* OpenapiTokens GET /api/security/openapi-tokens：令牌清单（安全页）。 */
func (c *AuthController) OpenapiTokens(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"tokens": c.Openapi.List()})
}

/* CreateOpenapiToken POST /api/security/openapi-tokens：生成命名令牌。 */
func (c *AuthController) CreateOpenapiToken(g *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t := c.Openapi.Create(req.Name)
	g.JSON(http.StatusOK, gin.H{"token": t})
}

/* RevokeOpenapiToken DELETE /api/security/openapi-tokens/:id：吊销令牌。 */
func (c *AuthController) RevokeOpenapiToken(g *gin.Context) {
	if err := c.Openapi.Revoke(g.Param("id")); err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	g.Status(http.StatusNoContent)
}

/* OpenapiAudit GET /api/security/openapi-audit?limit=：最近审计记录（倒序）。 */
func (c *AuthController) OpenapiAudit(g *gin.Context) {
	limit := 0
	if v := g.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	g.JSON(http.StatusOK, gin.H{"entries": service.ReadOpenapiAuditTail(limit)})
}

/*
sessionAuth 校验会话面：EzHarnessToken 头 / WS 子协议任一命中（恒定时间
比较）；只读请求另认 ez_token cookie（资产传输）。不认来源（Origin/Host
可伪造）——写接口的跨站防护靠"cookie 不参与写"。
*/
func sessionAuth(auth *service.AuthStore) gin.HandlerFunc {
	return func(g *gin.Context) {
		if validSession(g, auth, false) {
			g.Next()
			return
		}
		g.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}

/*
openGuard 校验 OpenAPI 面：EzHarnessOpenToken 命中任一枚令牌，或持有效
会话凭证（ezharness 前端与同源页面）；每笔调用落审计（含被拒的 401）。
*/
func openGuard(auth *service.AuthStore, openapi *service.OpenapiStore) gin.HandlerFunc {
	return func(g *gin.Context) {
		if t, ok := openapi.Verify(g.GetHeader(openapiTokenHeader)); ok {
			g.Set(ctxTokenID, t.ID)
			g.Set(ctxTokenName, t.Name)
		} else if validSession(g, auth, true) {
			g.Set(ctxTokenName, "会话")
		} else {
			/* 先写响应再审计：审计读 Writer.Status()，顺序反了会把 401 记成 200 */
			g.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			auditOpenapi(g)
			return
		}
		g.Next()
		auditOpenapi(g)
	}
}

/* ctxTokenID/Name 是 openGuard 写进 gin context 的审计字段名。 */
const (
	ctxTokenID   = "openapiTokenID"
	ctxTokenName = "openapiTokenName"
)

/* auditOpenapi 记一笔 openapi 调用审计（openGuard 专用）。 */
func auditOpenapi(g *gin.Context) {
	id, _ := g.Get(ctxTokenID)
	name, _ := g.Get(ctxTokenName)
	entryID, entryName := "", ""
	if s, ok := id.(string); ok {
		entryID = s
	}
	if s, ok := name.(string); ok {
		entryName = s
	}
	service.AppendOpenapiAudit(service.OpenapiAuditEntry{
		Method:    g.Request.Method,
		Path:      g.Request.URL.Path,
		TokenID:   entryID,
		TokenName: entryName,
		Status:    g.Writer.Status(),
	})
}

// validSession 收集会话凭证通道并逐一恒定时间比较。cookie 仅作资产传输
// 适配（导航、<img>/<video>/PDF、SSE 这类只读 HTTP 请求）：cookie 按
// host 生效、不看端口，不限制方法的话同机其他端口的页面能借用户浏览器
// 发起状态变更（CSRF）。WS 握手虽然也是 GET，但连上能发命令，所以同样
// 不接受 cookie——浏览器 WS 不能设自定义头，令牌走子协议；desktop 桥等
// 非浏览器客户端走 header。openGuard 传 allowCookieAny=true——快应用同源
// 页面的 POST 依赖 cookie，且 openapi 面本就对跨源开放、全量审计。
func validSession(g *gin.Context, auth *service.AuthStore, allowCookieAny bool) bool {
	if auth.VerifySession(g.GetHeader(sessionTokenHeader)) {
		return true
	}
	if t := wsToken(g.GetHeader("Sec-WebSocket-Protocol")); t != "" && auth.VerifySession(t) {
		return true
	}
	if isWebSocketUpgrade(g) {
		return false
	}
	readOnly := g.Request.Method == http.MethodGet || g.Request.Method == http.MethodHead
	if allowCookieAny || readOnly {
		if c, err := g.Cookie(authCookieName); err == nil && auth.VerifySession(c) {
			return true
		}
	}
	return false
}

/* isWebSocketUpgrade 判断这是不是 WS 握手（握手是 GET，cookie 对它不豁免）。 */
func isWebSocketUpgrade(g *gin.Context) bool {
	return strings.EqualFold(g.GetHeader("Upgrade"), "websocket")
}

/*
localOrigin 限制登录只接受本机页面发起：gin 的 JSON 绑定不看 Content-Type，
跨站页面可以用 text/plain 的简单请求 POST 到 127.0.0.1 换令牌。非浏览器
客户端（cli/desktop 主进程）不带 Origin，直接放行；Host 必须与 Origin
一致，防 DNS 重绑定。
*/
func localOrigin(g *gin.Context) bool {
	origin := g.GetHeader("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
	default:
		return false
	}
	return u.Host == g.Request.Host
}

/* clientIP 会话来源（安全页展示）；用 socket 地址，本机回环统一成 IPv4 写法。 */
func clientIP(g *gin.Context) string {
	ip := g.RemoteIP()
	if ip == "::1" {
		return "127.0.0.1"
	}
	return ip
}

/* currentToken 返回请求携带的会话令牌（logout 撤销自己用）。 */
func currentToken(g *gin.Context) string {
	if t := g.GetHeader(sessionTokenHeader); t != "" {
		return t
	}
	if c, err := g.Cookie(authCookieName); err == nil {
		return c
	}
	return wsToken(g.GetHeader("Sec-WebSocket-Protocol"))
}

// setAuthCookie 下发 HttpOnly 会话 cookie：SameSite=Strict 阻断跨站请求
// 顺手携带；与令牌同寿命（MaxAge 一年），浏览器重启后资产请求仍可用。
// HTTPS 下补 Secure。
func setAuthCookie(g *gin.Context, token string) {
	http.SetCookie(g.Writer, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   g.Request.TLS != nil,
		MaxAge:   365 * 24 * 60 * 60,
	})
}

// wsToken 从 Sec-WebSocket-Protocol 提取令牌：浏览器 WebSocket 不能设置
// 自定义请求头，客户端把令牌作为第二个子协议传入——
// new WebSocket(url, ["agent-auth", token])；agent-auth 必须排首位。
func wsToken(header string) string {
	parts := strings.Split(header, ",")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) != wsAuthProto {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// wsAcceptOptions 组装 WebSocket Accept 选项：放行本机开发来源；客户端
// 用子协议携带令牌时回显 agent-auth（ws 等客户端要求服务端确认所选协议）。
func wsAcceptOptions(g *gin.Context) *websocket.AcceptOptions {
	opts := &websocket.AcceptOptions{OriginPatterns: []string{"localhost:*", "127.0.0.1:*"}}
	if wsToken(g.GetHeader("Sec-WebSocket-Protocol")) != "" {
		opts.Subprotocols = []string{wsAuthProto}
	}
	return opts
}
