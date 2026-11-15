/* SessionController：bootstrap/status/history/fork。 */
package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/service"
)

/* SessionController 会话查询表现层。 */
type SessionController struct {
	Svc *service.SessionService
}

/* Bootstrap GET /api/bootstrap。 */
func (c *SessionController) Bootstrap(g *gin.Context) {
	g.JSON(http.StatusOK, c.Svc.Bootstrap(g.Request.Context()))
}

/* Status GET /api/status。 */
func (c *SessionController) Status(g *gin.Context) {
	g.JSON(http.StatusOK, c.Svc.Snapshot(g.Request.Context()))
}

/*
SetPrefs POST /api/sessions/:id/prefs（:id=分支根）：本会话的思考档位/
审批档位/主模型，body 可只带一个字段。模型变更要求会话空闲并重建该会话
装配（409 = 运行中或归档中）。
*/
func (c *SessionController) SetPrefs(g *gin.Context) {
	var body service.SessionPrefsPatch
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	v, err := c.Svc.SetPrefs(g.Request.Context(), g.Param("id"), body)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrBusy):
			g.JSON(http.StatusConflict, gin.H{"error": "会话运行中或归档中，先停止再切换模型"})
		case errors.Is(err, service.ErrTopicNotFound):
			g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}
	g.JSON(http.StatusOK, v)
}

/* HourlyStats GET /api/stats/hourly（按小时用量序列,模型面板趋势图）。 */
func (c *SessionController) HourlyStats(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"hours": c.Svc.Hub.Stats.Hourly()})
}

/*
History GET /api/sessions/:id（:id=分支根 ID，返回该线当前叶历史；
tail=N 只回尾部 N 条消息，start 给出偏移，锚点对齐用）。
*/
func (c *SessionController) History(g *gin.Context) {
	tail, _ := strconv.Atoi(g.Query("tail"))
	g.JSON(http.StatusOK, c.Svc.History(g.Request.Context(), g.Param("id"), tail))
}

/* Prev GET /api/sessions/:id/prev（compact 链上一会话，无上级 204）。 */
func (c *SessionController) Prev(g *gin.Context) {
	d, ok, err := c.Svc.Prev(g.Request.Context(), g.Param("id"))
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	if !ok {
		g.Status(http.StatusNoContent)
		return
	}
	g.JSON(http.StatusOK, d)
}

/* Fork GET /api/sessions/:id/forks/:fid（fork 分身详情，抽屉懒加载）。 */
func (c *SessionController) Fork(g *gin.Context) {
	d, err := c.Svc.Fork(g.Request.Context(), g.Param("id"), g.Param("fid"))
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, d)
}
