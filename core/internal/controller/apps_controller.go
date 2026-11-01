/* AppsController：快应用列表与启动（开窗由 desktop 壳/web 端执行）。 */
package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/service"
)

/* AppsController 快应用表现层。 */
type AppsController struct {
	Svc *service.AppsService
}

/* List GET /api/apps。 */
func (c *AppsController) List(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"apps": c.Svc.List(g.Request.Context())})
}

/*
Open POST /api/apps/open {name}：启动快应用——返回前端入口路径与（声明了
backend 时的）后端终端 id，开窗由调用方执行。名单校验在 service（只有
apps/ 下含 app.quick 的目录算应用、名字不可穿越；chip 的 id 来自模型
输出不可信）。
*/
func (c *AppsController) Open(g *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	view, err := c.Svc.Launch(g.Request.Context(), req.Name)
	if errors.Is(err, service.ErrAppNotFound) {
		g.JSON(http.StatusNotFound, gin.H{"error": "app not found"})
		return
	}
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := gin.H{"ok": true, "path": view.Path, "title": view.Title + " · ezharness"}
	if view.TermID != "" {
		out["termId"] = view.TermID // 纯前端应用不带该字段
	}
	g.JSON(http.StatusOK, out)
}

/* Pin POST /api/apps/:name/pin：钉到桌面小助手/取消（toggle），返回新状态。 */
func (c *AppsController) Pin(g *gin.Context) {
	pinned, err := c.Svc.TogglePin(g.Request.Context(), g.Param("name"))
	if errors.Is(err, service.ErrAppNotFound) {
		g.JSON(http.StatusNotFound, gin.H{"error": "app not found"})
		return
	}
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "pinned": pinned})
}

/*
Uninstall DELETE /api/apps/:name：用户卸载快应用——停仍在跑的后端终端、
删 apps/<名>/ 目录、清钉选残留。名单校验在 service（名字不可穿越）。
*/
func (c *AppsController) Uninstall(g *gin.Context) {
	if err := c.Svc.Uninstall(g.Request.Context(), g.Param("name")); err != nil {
		if errors.Is(err, service.ErrAppNotFound) {
			g.JSON(http.StatusNotFound, gin.H{"error": "app not found"})
			return
		}
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}
