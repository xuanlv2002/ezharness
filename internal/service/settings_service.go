/*
SettingsService 与 MemoryService：运行配置与长期记忆的读写用例。
*/
package service

import (
	"context"
	"errors"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
)

/* SettingsService 设置用例。 */
type SettingsService struct {
	Hub    *domain.Hub
	Agents *AgentService
}

/* Get 返回当前设置。 */
func (s *SettingsService) Get() domain.Settings { return s.Hub.SettingsSnapshot() }

/* Update 保存设置并重建 agent（busy 时拒绝）。 */
func (s *SettingsService) Update(st domain.Settings) error {
	if st.Model == "" || st.BaseURL == "" {
		return errors.New("model and baseUrl required")
	}
	if err := domain.SaveSettings(s.Hub.Fsys, st); err != nil {
		return err
	}
	s.Hub.ApplySettings(st)
	return s.Agents.Reassemble(st)
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
