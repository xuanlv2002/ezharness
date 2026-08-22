/*
SettingsService 与 MemoryService：运行配置与长期记忆的读写用例。
*/
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/hook/localsession"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
	"ezharness/internal/osfs"
)

/* SettingsService 设置用例。 */
type SettingsService struct {
	Hub    *domain.Hub
	Agents *AgentService
}

/* SettingsView 是设置页行为设置视图（模型归 /api/models）。 */
type SettingsView struct {
	SystemExtra string `json:"systemExtra"`
}

/* Get 返回当前行为设置。 */
func (s *SettingsService) Get() SettingsView {
	st := s.Hub.SettingsSnapshot()
	return SettingsView{SystemExtra: st.SystemExtra}
}

/* Update 保存行为设置并重建 agent（busy 时拒绝）。 */
func (s *SettingsService) Update(v SettingsView) error {
	st := s.Hub.SettingsSnapshot()
	st.SystemExtra = v.SystemExtra
	if err := domain.SaveSettings(s.Hub.Fsys, st); err != nil {
		return err
	}
	s.Hub.ApplySettings(st)
	return s.Agents.Reassemble(st)
}

/* GetModels 返回模型四槽（空槽归一为 [] 而非 null）。 */
func (s *SettingsService) GetModels() domain.ModelsConfig {
	return normalizeModels(s.Hub.ModelsSnapshot())
}

/* normalizeModels 把 nil 槽归一为空切片（JSON null → []）。 */
func normalizeModels(m domain.ModelsConfig) domain.ModelsConfig {
	if m.Main == nil {
		m.Main = []domain.ModelEntry{}
	}
	if m.Vision == nil {
		m.Vision = []domain.ModelEntry{}
	}
	if m.Image == nil {
		m.Image = []domain.ModelEntry{}
	}
	if m.Audio == nil {
		m.Audio = []domain.ModelEntry{}
	}
	return m
}

/* UpdateModels 保存模型四槽（每槽至多一条启用，main 槽不可为空）
并重建 agent（busy 时拒绝）。 */
func (s *SettingsService) UpdateModels(m domain.ModelsConfig) error {
	if len(m.Main) == 0 {
		return errors.New("main 槽至少需要一个模型")
	}
	for slot, entries := range map[string][]domain.ModelEntry{
		"main": m.Main, "vision": m.Vision, "image": m.Image, "audio": m.Audio,
	} {
		enabled := 0
		for _, e := range entries {
			if e.Enabled {
				enabled++
			}
		}
		if enabled > 1 {
			return fmt.Errorf("%s 槽至多启用一个模型", slot)
		}
	}
	if err := domain.SaveModelsConfig(s.Hub.Fsys, normalizeModels(m)); err != nil {
		return err
	}
	s.Hub.ApplyModels(m)
	return s.Agents.Reassemble(s.Hub.SettingsSnapshot())
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

/* FileInfoView 是记忆文件条目。 */
type FileInfoView struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Mtime string `json:"mtime"`
}

/* SkillEntryView 是能力记忆（skill）条目。 */
type SkillEntryView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Desc    string `json:"desc"`
	Enabled bool   `json:"enabled"`
}

/* MemoryConfigView 是记忆页数据：三文件夹（长期记忆/能力记忆/话题记忆）。 */
type MemoryConfigView struct {
	Longterm struct {
		Dir       string         `json:"dir"`
		HarnessMd *FileInfoView  `json:"harnessMd"`
		Files     []FileInfoView `json:"files"`
	} `json:"longterm"`
	Skills struct {
		Dir   string            `json:"dir"`
		Items []SkillEntryView  `json:"items"`
	} `json:"skills"`
	Topics struct {
		Dir   string              `json:"dir"`
		Items []hooks.TopicEntry  `json:"items"`
	} `json:"topics"`
}

/* Config 汇总记忆页数据（目录缺失容错为空列表）。 */
func (m *MemoryService) Config() MemoryConfigView {
	var v MemoryConfigView
	v.Longterm.Dir = hooks.LongtermDir
	v.Skills.Dir = hooks.SkillsDir
	v.Topics.Dir = localsession.DefaultDir

	if entries, err := m.Hub.Fsys.List(context.Background(), hooks.LongtermDir); err == nil {
		for _, e := range entries {
			if e.IsDir {
				continue
			}
			fi := FileInfoView{Name: e.Name, Size: e.Size, Mtime: fileMtime(hooks.LongtermDir + "/" + e.Name)}
			if e.Name == "harness.md" {
				v.Longterm.HarnessMd = &fi
			} else {
				v.Longterm.Files = append(v.Longterm.Files, fi)
			}
		}
	}
	if entries, err := m.Hub.Fsys.List(context.Background(), hooks.SkillsDir); err == nil {
		for _, e := range entries {
			if e.IsDir || !strings.HasSuffix(e.Name, ".md") {
				continue
			}
			name := strings.TrimSuffix(e.Name, ".md")
			v.Skills.Items = append(v.Skills.Items, SkillEntryView{
				ID:      name,
				Name:    name,
				Desc:    skillDesc(m.Hub.Fsys, hooks.SkillsDir+"/"+e.Name),
				Enabled: true, // 启停机制待后续（skill hook 无开关，暂恒启用）
			})
		}
	}
	v.Topics.Items = m.Hub.Topics.Load()
	if v.Longterm.Files == nil {
		v.Longterm.Files = []FileInfoView{}
	}
	if v.Skills.Items == nil {
		v.Skills.Items = []SkillEntryView{}
	}
	if v.Topics.Items == nil {
		v.Topics.Items = []hooks.TopicEntry{}
	}
	return v
}

/* skillDesc 取 skill 文件首行标题（# 开头）作为描述。 */
func skillDesc(fsys osfs.OS, path string) string {
	data, err := fsys.Read(context.Background(), path)
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return strings.TrimPrefix(strings.TrimPrefix(line, "#"), " ")
		}
	}
	return ""
}

func fileMtime(path string) string {
	if fi, err := os.Stat(path); err == nil {
		return fi.ModTime().Format("2006-01-02 15:04")
	}
	return ""
}

/* GetMemory 返回 harness.md 内容。 */
func (m *MemoryService) GetMemory() string {
	data, _ := m.Hub.Fsys.Read(context.Background(), hooks.MemoryFile)
	return string(data)
}

/* SaveMemory 写入 harness.md（下一轮对话即注入 system）。 */
func (m *MemoryService) SaveMemory(content string) error {
	return m.Hub.Fsys.Write(context.Background(), hooks.MemoryFile, []byte(content))
}
