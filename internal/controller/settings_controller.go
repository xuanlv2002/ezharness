/* SettingsController：设置与长期记忆。 */
package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/internal/domain"
	"ezharness/internal/service"
)

/* SettingsController 设置表现层。 */
type SettingsController struct {
	Settings *service.SettingsService
	Memory   *service.MemoryService
}

/* GetSettings GET /api/settings。 */
func (c *SettingsController) GetSettings(g *gin.Context) {
	g.JSON(http.StatusOK, c.Settings.Get())
}

/* UpdateSettings POST /api/settings。 */
func (c *SettingsController) UpdateSettings(g *gin.Context) {
	var st domain.Settings
	if err := g.ShouldBindJSON(&st); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Settings.Update(st); err != nil {
		if err.Error() == "model and baseUrl required" {
			g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		g.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* GetMemory GET /api/memory。 */
func (c *SettingsController) GetMemory(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"content": c.Memory.GetMemory()})
}

/* SaveMemory POST /api/memory。 */
func (c *SettingsController) SaveMemory(g *gin.Context) {
	var body struct {
		Content string `json:"content"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Memory.SaveMemory(body.Content); err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}
