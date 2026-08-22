/* TopicController：话题存档查询与恢复。 */
package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/internal/domain"
	"ezharness/internal/service"
)

/* TopicController 话题表现层。 */
type TopicController struct {
	Svc *service.TopicService
}

/* List GET /api/topics。 */
func (c *TopicController) List(g *gin.Context) {
	g.JSON(http.StatusOK, c.Svc.List())
}

/* Get GET /api/topics/:id。 */
func (c *TopicController) Get(g *gin.Context) {
	d, err := c.Svc.Get(g.Request.Context(), g.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrTopicNotFound) {
			g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, d)
}

/* Delete DELETE /api/topics/:id。 */
func (c *TopicController) Delete(g *gin.Context) {
	if err := c.Svc.Delete(g.Param("id")); err != nil {
		if errors.Is(err, service.ErrTopicNotFound) {
			g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* Resume POST /api/topics/:id/resume。 */
func (c *TopicController) Resume(g *gin.Context) {
	if err := c.Svc.Resume(g.Request.Context(), g.Param("id")); err != nil {
		switch {
		case errors.Is(err, domain.ErrBusy):
			g.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrTopicNotFound):
			g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	g.JSON(http.StatusOK, gin.H{"id": c.Svc.Hub.Active.ID})
}
