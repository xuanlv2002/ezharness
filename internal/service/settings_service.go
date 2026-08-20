/*
SettingsService 与 MemoryService：运行配置与长期记忆的读写用例。
*/
package service

import (
	"context"
	"errors"
	"fmt"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
)

/* SettingsService 设置用例。 */
type SettingsService struct {
	Hub    *domain.Hub
	Agents *AgentService
}

/* SettingsView 是设置页合并视图（models.json + settings.json）。 */
type SettingsView struct {
	APIKey          string `json:"apiKey"`
	Model           string `json:"model"`
	BaseURL         string `json:"baseUrl"`
	SystemExtra     string `json:"systemExtra"`
	RotateThreshold int    `json:"rotateThreshold"`
	Shell           string `json:"shell"`
}

/* Get 返回当前设置（模型配置 + 行为设置合并视图）。 */
func (s *SettingsService) Get() SettingsView {
	m, st := s.Hub.ModelSnapshot(), s.Hub.SettingsSnapshot()
	return SettingsView{
		APIKey:          m.APIKey,
		Model:           m.Model,
		BaseURL:         m.BaseURL,
		SystemExtra:     st.SystemExtra,
		RotateThreshold: st.RotateThreshold,
		Shell:           st.Shell,
	}
}

/* Update 保存设置（分写 models.json 与 settings.json）并重建 agent（busy 时拒绝）。 */
func (s *SettingsService) Update(v SettingsView) error {
	if v.Model == "" || v.BaseURL == "" {
		return errors.New("model and baseUrl required")
	}
	mc := domain.ModelConfig{APIKey: v.APIKey, Model: v.Model, BaseURL: v.BaseURL}
	st := s.Hub.SettingsSnapshot()
	st.SystemExtra = v.SystemExtra
	st.RotateThreshold = v.RotateThreshold
	st.Shell = v.Shell
	if err := domain.SaveModelConfig(s.Hub.Fsys, mc); err != nil {
		return err
	}
	if err := domain.SaveSettings(s.Hub.Fsys, st); err != nil {
		return err
	}
	s.Hub.ApplyModel(mc)
	s.Hub.ApplySettings(st)
	return s.Agents.Reassemble(mc, st)
}

/* SecurityRules 返回当前审批策略。 */
func (s *SettingsService) SecurityRules() []domain.ToolRule {
	return s.Hub.SettingsSnapshot().ToolRules
}

/* UpdateSecurity 保存审批策略。needsApprove 运行时读设置快照，即时生效。 */
func (s *SettingsService) UpdateSecurity(rules []domain.ToolRule) error {
	valid := map[domain.Level]bool{
		domain.LevelAsk: true, domain.LevelBlack: true,
		domain.LevelWhite: true, domain.LevelAuto: true,
	}
	for _, r := range rules {
		if !valid[r.Level] {
			return fmt.Errorf("非法档位 %q（tool %s）", r.Level, r.Tool)
		}
		if r.Tool == "task" && (r.Level == domain.LevelBlack || r.Level == domain.LevelWhite) {
			return errors.New("task 只支持 审批/免审（分身继承主 agent 策略）")
		}
	}
	st := s.Hub.SettingsSnapshot()
	st.ToolRules = rules
	if err := domain.SaveSettings(s.Hub.Fsys, st); err != nil {
		return err
	}
	s.Hub.ApplySettings(st)
	return nil
}

/* MemoryService 记忆用例。 */
type MemoryService struct {
	Hub *domain.Hub
}

/* GetMemory 返回 memory.md 内容。 */
func (m *MemoryService) GetMemory() string {
	data, _ := m.Hub.Fsys.Read(context.Background(), hooks.MemoryFile)
	return string(data)
}

/* SaveMemory 写入 memory.md（下一轮对话即注入 system）。 */
func (m *MemoryService) SaveMemory(content string) error {
	return m.Hub.Fsys.Write(context.Background(), hooks.MemoryFile, []byte(content))
}
