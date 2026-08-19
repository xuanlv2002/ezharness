/* SessionController：bootstrap/status/history/summary。 */
package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/internal/service"
)

/* SessionController 会话查询表现层。 */
type SessionController struct {
	Svc *service.SessionService
}

/* Bootstrap GET /api/bootstrap。 */
func (c *SessionController) Bootstrap(g *gin.Context) {
	g.JSON(http.StatusOK, c.Svc.Bootstrap())
}

/* Status GET /api/status。 */
func (c *SessionController) Status(g *gin.Context) {
	g.JSON(http.StatusOK, c.Svc.Snapshot())
}

/* History GET /api/sessions/:id（仅活动会话）。 */
func (c *SessionController) History(g *gin.Context) {
	if g.Param("id") != c.Svc.Hub.Active.ID {
		g.JSON(http.StatusNotFound, gin.H{"error": "session not active"})
		return
	}
	g.JSON(http.StatusOK, c.Svc.History())
}

/* Summary POST /api/sessions/:id/summary。 */
func (c *SessionController) Summary(g *gin.Context) {
	text, err := c.Svc.Summarize(g.Request.Context())
	if err != nil {
		if errors.Is(err, service.ErrEmptySession) {
			g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"text": text})
}
