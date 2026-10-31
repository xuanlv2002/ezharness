/* SettingsController：设置与长期记忆。 */
package controller

import (
	"encoding/base64"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
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
	if err := c.Settings.Update(g.Request.Context(), st); err != nil {
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
	if err := c.Settings.UpdateModels(g.Request.Context(), m); err != nil {
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
	if err := c.Settings.UpdateSecurity(g.Request.Context(), body.Rules); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* GetMemoryConfig GET /api/memory/config（记忆页技能清单与会话目录）。 */
func (c *SettingsController) GetMemoryConfig(g *gin.Context) {
	g.JSON(http.StatusOK, c.Memory.Config(g.Request.Context()))
}

/* GetLongterm GET /api/memory/longterm（条目式长期记忆全量）。 */
func (c *SettingsController) GetLongterm(g *gin.Context) {
	v, err := c.Memory.Longterm(g.Request.Context())
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, v)
}

/*
SaveLongtermItems POST /api/memory/longterm/:target（user|soul 整组保存）。

	只收 id/text：id 空为新增（服务端补号），updatedAt 由服务端盖章。
*/
func (c *SettingsController) SaveLongtermItems(g *gin.Context) {
	var body struct {
		Items []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"items"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items := make([]hooks.MemoryItem, 0, len(body.Items))
	for _, it := range body.Items {
		items = append(items, hooks.MemoryItem{ID: it.ID, Text: it.Text})
	}
	if err := c.Memory.SaveLongtermItems(g.Request.Context(), g.Param("target"), items); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "items": items})
}

/* GetProject GET /api/memory/project/:id（项目记忆详情）。 */
func (c *SettingsController) GetProject(g *gin.Context) {
	v, err := c.Memory.GetProject(g.Request.Context(), g.Param("id"))
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, v)
}

/* SaveProject POST /api/memory/project（新建/更新项目记忆）。 */
func (c *SettingsController) SaveProject(g *gin.Context) {
	var body struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := c.Memory.SaveProject(g.Request.Context(), body.ID, body.Name, body.Description, body.Content)
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "id": id})
}

/* DeleteProject DELETE /api/memory/project/:id。 */
func (c *SettingsController) DeleteProject(g *gin.Context) {
	if err := c.Memory.DeleteProject(g.Request.Context(), g.Param("id")); err != nil {
		if os.IsNotExist(err) {
			g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* DecideMemoryPending POST /api/memory/pending/:id/decide，body {accept}（确认应用/取消丢弃）。 */
func (c *SettingsController) DecideMemoryPending(g *gin.Context) {
	var body struct {
		Accept bool `json:"accept"`
	}
	_ = g.ShouldBindJSON(&body) // 空体按取消处理
	result, err := c.Memory.DecideMemoryPending(g.Request.Context(), g.Param("id"), body.Accept)
	if err != nil {
		if os.IsNotExist(err) {
			g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "result": result})
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
	if err := c.Memory.CreateSkill(g.Request.Context(), data); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* GetSkill GET /api/memory/skills/:id（清单字段 + SKILL.md 正文）。 */
func (c *SettingsController) GetSkill(g *gin.Context) {
	v, err := c.Memory.SkillContent(g.Request.Context(), g.Param("id"))
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, v)
}

/* DeleteSkill DELETE /api/memory/skills/:id。 */
func (c *SettingsController) DeleteSkill(g *gin.Context) {
	if err := c.Memory.DeleteSkill(g.Request.Context(), g.Param("id")); err != nil {
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
	if err := c.Memory.ToggleSkill(g.Request.Context(), g.Param("id"), body.Enabled); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* ActivateModel POST /api/models/activate：主模型切换（输入框选择器；下一次输入生效）。 */
func (c *SettingsController) ActivateModel(g *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Name == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	e, err := c.Settings.ActivateMainModel(g.Request.Context(), req.Name)
	if err != nil {
		g.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "model": e})
}

/* SetReasoning POST /api/settings/reasoning：思考强度档位（输入框下拉；请求侧实时读取，即改即生效）。 */
func (c *SettingsController) SetReasoning(g *gin.Context) {
	var req struct {
		Effort string `json:"effort"`
	}
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Settings.SetReasoningEffort(g.Request.Context(), req.Effort); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "effort": req.Effort})
}

/* SetApproveMode POST /api/settings/approve-mode：工具审批档位（输入框下拉；工具审批实时读取，即改即生效）。 */
func (c *SettingsController) SetApproveMode(g *gin.Context) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Settings.SetApproveMode(g.Request.Context(), req.Mode); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "mode": req.Mode})
}

/*
CallModel POST /api/open/model/call：直调大模型（OpenAPI 面，与 /api/open/mcp/call 同级
系统内置能力）。body {model?, prompt, images?: [路径]}；返回 {result}。
*/
func (c *SettingsController) CallModel(g *gin.Context) {
	var req struct {
		Model  string   `json:"model"`
		Prompt string   `json:"prompt"`
		Images []string `json:"images"`
	}
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := c.Settings.ModelCall(g.Request.Context(), req.Model, req.Prompt, req.Images)
	if err != nil {
		g.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"result": result})
}
