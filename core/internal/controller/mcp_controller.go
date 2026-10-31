/* McpController：MCP 配置读写与页面侧会话（连接/调用）。 */
package controller

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/service"
)

/* McpController MCP 表现层。 */
type McpController struct {
	Svc *service.McpService
}

/* List GET /api/mcp。 */
func (c *McpController) List(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"servers": c.Svc.List(g.Request.Context())})
}

/*
OpenList GET /api/open/mcp：OpenAPI 面的服务清单，只报名字/描述/启用——
不含连接配置（env/header 可能带密钥），openapi 令牌持有者不可见。
*/
func (c *McpController) OpenList(g *gin.Context) {
	type lean struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Enabled     bool   `json:"enabled"`
	}
	servers := c.Svc.List(g.Request.Context())
	out := make([]lean, 0, len(servers))
	for _, s := range servers {
		out = append(out, lean{Name: s.Name, Description: s.Description, Enabled: s.Enabled})
	}
	g.JSON(http.StatusOK, gin.H{"servers": out})
}

/* Update POST /api/mcp（下一轮迭代热加载生效）。 */
func (c *McpController) Update(g *gin.Context) {
	var f service.McpFile
	if err := g.ShouldBindJSON(&f); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Svc.Update(g.Request.Context(), f); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* Connect POST /api/mcp/connect：建立页面会话并返回工具清单。 */
func (c *McpController) Connect(g *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Name == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	tools, err := c.Svc.Connect(g.Request.Context(), req.Name)
	if err != nil {
		var ar *service.McpAuthRequiredError
		if errors.As(err, &ar) {
			c.authRequired(g, ar.Server, ar.MetadataURL)
			return
		}
		g.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"tools": tools})
}

/* Disconnect POST /api/mcp/disconnect。 */
func (c *McpController) Disconnect(g *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Name == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	c.Svc.Disconnect(req.Name)
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* Call POST /api/mcp/call：经页面会话调用 MCP 工具。 */
func (c *McpController) Call(g *gin.Context) {
	var req struct {
		Server string          `json:"server"`
		Tool   string          `json:"tool"`
		Args   json.RawMessage `json:"args"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Server == "" || req.Tool == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "server and tool required"})
		return
	}
	result, err := c.Svc.Call(g.Request.Context(), req.Server, req.Tool, req.Args)
	if err != nil {
		var ar *service.McpAuthRequiredError
		if errors.As(err, &ar) {
			c.authRequired(g, ar.Server, ar.MetadataURL)
			return
		}
		g.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"result": result})
}

/*
authRequired 回 HTTP 401 + needs_auth 契约（快应用/脚本据此引导 OAuth）：
{error, needs_auth, server, auth_url, metadata_url}。auth_url 现取（幂等），
失败则留空供调用方自取；metadata_url 是 RFC9728 发现文档地址（可空）。
*/
func (c *McpController) authRequired(g *gin.Context, server, metadataURL string) {
	url := ""
	if c.Svc.Auth != nil {
		if u, err := c.Svc.Auth.Start(g.Request.Context(), server, metadataURL); err == nil {
			url = u
		}
	}
	g.JSON(http.StatusUnauthorized, gin.H{
		"error":        "需要 OAuth 认证",
		"needs_auth":   true,
		"server":       server,
		"auth_url":     url,
		"metadata_url": metadataURL,
	})
}

/* OAuthStart POST /api/mcp/oauth/start：返回授权 URL（前端用系统浏览器打开）。 */
func (c *McpController) OAuthStart(g *gin.Context) {
	var req struct {
		Name        string `json:"name"`
		MetadataURL string `json:"metadata_url"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Name == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	if c.Svc.Auth == nil {
		g.JSON(http.StatusBadGateway, gin.H{"error": "oauth unavailable"})
		return
	}
	url, err := c.Svc.Auth.Start(g.Request.Context(), req.Name, req.MetadataURL)
	if err != nil {
		g.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"url": url})
}

/* OAuthProbe POST /api/mcp/oauth/probe：探测该 server 是否需要 OAuth（新建后亮徽标）。 */
func (c *McpController) OAuthProbe(g *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Name == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	g.JSON(http.StatusOK, gin.H{"oauth": c.Svc.ProbeOAuth(g.Request.Context(), req.Name)})
}

/* OAuthStatus GET /api/mcp/oauth/status?name=：授权状态（前端轮询）。 */
func (c *McpController) OAuthStatus(g *gin.Context) {
	name := g.Query("name")
	if name == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	authorized, pending := false, false
	if c.Svc.Auth != nil {
		authorized, pending = c.Svc.Auth.Status(g.Request.Context(), name)
	}
	g.JSON(http.StatusOK, gin.H{"authorized": authorized, "pending": pending})
}

/* OAuthCancel POST /api/mcp/oauth/cancel：取消待完成的授权流程。 */
func (c *McpController) OAuthCancel(g *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Name == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	if c.Svc.Auth != nil {
		c.Svc.Auth.Cancel(req.Name)
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* OAuthLogout POST /api/mcp/oauth/logout：删除令牌并断开该 server。 */
func (c *McpController) OAuthLogout(g *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Name == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	if c.Svc.Auth != nil {
		c.Svc.Auth.Logout(g.Request.Context(), req.Name)
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}
