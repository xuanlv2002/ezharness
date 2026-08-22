/* McpController：MCP 配置读写。 */
package controller

import (
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
