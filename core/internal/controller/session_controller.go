/* SessionController：bootstrap/status/history/fork。 */
package controller

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

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
