/* McpController：MCP 配置读写与页面侧会话（连接/调用）。 */
package controller

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/internal/service"
)

/* McpController MCP 表现层。 */
type McpController struct {
	Svc *service.McpService
}

/* List GET /api/mcp。 */
func (c *McpController) List(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"servers": c.Svc.List()})
}

/* Update POST /api/mcp（下一轮迭代热加载生效）。 */
func (c *McpController) Update(g *gin.Context) {
	var f service.McpFile
	if err := g.ShouldBindJSON(&f); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Svc.Update(f); err != nil {
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
		g.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"result": result})
}
