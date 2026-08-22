/* AppsController：快应用列表。 */
package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/internal/service"
)

/* AppsController 快应用表现层。 */
type AppsController struct {
	Svc *service.AppsService
}

/* List GET /api/apps。 */
func (c *AppsController) List(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"apps": c.Svc.List()})
}
