/* SettingsController：设置与长期记忆。 */
package controller

import (
	"encoding/base64"
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/service"
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
	var st service.SettingsView
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

/* GetModels GET /api/models。 */
func (c *SettingsController) GetModels(g *gin.Context) {
	g.JSON(http.StatusOK, c.Settings.GetModels())
}

/* UpdateModels POST /api/models（每槽至多一条启用，保存后重建 agent）。 */
func (c *SettingsController) UpdateModels(g *gin.Context) {
	var m domain.ModelsConfig
	if err := g.ShouldBindJSON(&m); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Settings.UpdateModels(m); err != nil {
		g.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* GetSecurity GET /api/security。 */
func (c *SettingsController) GetSecurity(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"rules": c.Settings.SecurityRules()})
}

/* UpdateSecurity POST /api/security。 */
func (c *SettingsController) UpdateSecurity(g *gin.Context) {
	var body struct {
		Rules []domain.ToolRule `json:"rules"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Settings.UpdateSecurity(body.Rules); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* GetMemoryConfig GET /api/memory/config（记忆页三文件夹数据）。 */
func (c *SettingsController) GetMemoryConfig(g *gin.Context) {
	g.JSON(http.StatusOK, c.Memory.Config())
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

/* CreateSkill POST /api/memory/skills（zip base64 新建技能，名称自动推导）。 */
func (c *SettingsController) CreateSkill(g *gin.Context) {
	var body struct {
		Data string `json:"data"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, err := base64.StdEncoding.DecodeString(body.Data)
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "data 不是有效的 base64"})
		return
	}
	if err := c.Memory.CreateSkill(data); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* DeleteSkill DELETE /api/memory/skills/:id。 */
func (c *SettingsController) DeleteSkill(g *gin.Context) {
	if err := c.Memory.DeleteSkill(g.Param("id")); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* ToggleSkill POST /api/memory/skills/:id/enabled（启停技能）。 */
func (c *SettingsController) ToggleSkill(g *gin.Context) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Memory.ToggleSkill(g.Param("id"), body.Enabled); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}
