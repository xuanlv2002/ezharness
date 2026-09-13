/* AppsController：快应用列表与开窗校验（窗口由 desktop 壳执行）。 */
package controller

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/service"
)

/* AppsController 快应用表现层。 */
type AppsController struct {
	Svc *service.AppsService
}

/* List GET /api/apps。 */
func (c *AppsController) List(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"apps": c.Svc.List()})
}

/*
Open POST /api/apps/open {name}：名单校验（杜绝路径穿越；chip 的 id 来自
模型输出不可信）后返回路径与标题，开窗由调用方（desktop 壳/web 新标签页）
执行。
*/
func (c *AppsController) Open(g *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for _, e := range c.Svc.List() {
		// 名单是带 .html 后缀的文件名；app:// 引用与 save_app 的 name 均
		// 为无后缀短名——两种写法都接受（仍限定名单内，不可穿越）
		if e.Name == req.Name || strings.TrimSuffix(e.Name, ".html") == req.Name {
			g.JSON(http.StatusOK, gin.H{"ok": true, "path": "/apps/" + e.Name, "title": e.Title + " · ezharness"})
			return
		}
	}
	g.JSON(http.StatusNotFound, gin.H{"error": "app not found"})
}
