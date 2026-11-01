/* CallbackController：定时回调中心的任务 CRUD、启停与立即运行。 */
package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/callback"
	"ezharness/core/internal/service"
)

/* CallbackController 定时回调表现层。 */
type CallbackController struct {
	Svc *service.CallbackService
}

/* List GET /api/callbacks?sessionId=&app=：清单（可按会话 ID 或快应用名过滤）。 */
func (c *CallbackController) List(g *gin.Context) {
	tasks := c.Svc.List()
	sessionID := g.Query("sessionId")
	app := g.Query("app")
	out := make([]callback.Task, 0, len(tasks))
	for _, t := range tasks {
		if sessionID != "" && (t.Kind != callback.KindSession || t.Session == nil ||
			t.Session.Mode != callback.ModeSession || t.Session.SessionID != sessionID) {
			continue
		}
		if app != "" && (t.Kind != callback.KindApp || t.App == nil || t.App.Name != app) {
			continue
		}
		out = append(out, t)
	}
	g.JSON(http.StatusOK, gin.H{"callbacks": out})
}

/* Create POST /api/callbacks。 */
func (c *CallbackController) Create(g *gin.Context) {
	var in callback.CreateInput
	if err := g.ShouldBindJSON(&in); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := c.Svc.Create(g.Request.Context(), in)
	if err != nil {
		writeCallbackErr(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"callback": t})
}

/* Update POST /api/callbacks/:id（只改传入字段）。 */
func (c *CallbackController) Update(g *gin.Context) {
	var in callback.UpdateInput
	if err := g.ShouldBindJSON(&in); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := c.Svc.Update(g.Request.Context(), g.Param("id"), in)
	if err != nil {
		writeCallbackErr(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"callback": t})
}

/* Delete DELETE /api/callbacks/:id。 */
func (c *CallbackController) Delete(g *gin.Context) {
	if err := c.Svc.Delete(g.Request.Context(), g.Param("id")); err != nil {
		writeCallbackErr(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* SetEnabled POST /api/callbacks/:id/enabled body {enabled}。 */
func (c *CallbackController) SetEnabled(g *gin.Context) {
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if err := g.ShouldBindJSON(&in); err != nil || in.Enabled == nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "enabled required"})
		return
	}
	t, err := c.Svc.SetEnabled(g.Request.Context(), g.Param("id"), *in.Enabled)
	if err != nil {
		writeCallbackErr(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"callback": t})
}

/* Run POST /api/callbacks/:id/run：立即执行一次（后台，不改变计划）。 */
func (c *CallbackController) Run(g *gin.Context) {
	if err := c.Svc.RunNow(g.Param("id")); err != nil {
		writeCallbackErr(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* writeCallbackErr 把服务错误映射到 HTTP 状态码。 */
func writeCallbackErr(g *gin.Context, err error) {
	switch {
	case errors.Is(err, callback.ErrNotFound):
		g.JSON(http.StatusNotFound, gin.H{"error": "定时回调不存在"})
	case errors.Is(err, service.ErrCallbackInvalid), errors.Is(err, service.ErrCallbackFinished):
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrCallbackRunning):
		g.JSON(http.StatusConflict, gin.H{"error": "回调正在执行，稍后再试"})
	default:
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
