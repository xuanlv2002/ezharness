/*
配置用例：配置记录（models.json/settings.json/toolRules.json）的加载与
落盘归 service 层——domain 是纯内存聚合，只在装配点（BootstrapHub）与
变更用例（Update/用量累计）触盘。
*/
package service

import (
	"context"
	"encoding/json"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
	"ezharness/core/internal/osfs"
)

/* BootstrapHub 装配领域根：加载配置记录（缺失文件自动写默认值）、生命体征与话题索引，并恢复活动会话（main 每代调用）。 */
func BootstrapHub(h *domain.Hub) {
	ctx := context.Background()
	if _, err := h.Fsys.Read(ctx, "models.json"); err != nil {
		h.Models = domain.DefaultModelsConfig()
		_ = SaveModelsConfig(h.Fsys, h.Models)
	} else {
		h.Models = LoadModelsConfig(h.Fsys)
	}
	if _, err := h.Fsys.Read(ctx, "settings.json"); err != nil {
		h.Settings = domain.DefaultSettings()
		_ = SaveSettings(h.Fsys, h.Settings)
	} else {
		h.Settings = LoadSettings(h.Fsys)
	}
	if _, err := h.Fsys.Read(ctx, "toolRules.json"); err != nil {
		h.ToolRules = domain.DefaultToolRules()
		_ = SaveToolRules(h.Fsys, h.ToolRules)
	} else {
		h.ToolRules = LoadToolRules(h.Fsys)
	}
	h.Stats = domain.NewStats(h.Fsys)
	h.Topics = hooks.NewTopics(h.Fsys)
	h.SetActive(h.BootstrapActive())
}

/* recordUsage 累计主模型用量并落盘 models.json（chat 每轮调用）。 */
func recordUsage(h *domain.Hub, u *types.Usage) {
	if h.ApplyUsage(u) {
		_ = SaveModelsConfig(h.Fsys, h.ModelsSnapshot())
	}
}

/* LoadModelsConfig 读 models.json，缺失或损坏回落默认值。 */
func LoadModelsConfig(fsys osfs.OS) domain.ModelsConfig {
	data, err := fsys.Read(context.Background(), "models.json")
	if err != nil {
		return domain.DefaultModelsConfig()
	}
	var mc domain.ModelsConfig
	if json.Unmarshal(data, &mc) == nil && (len(mc.Main)+len(mc.Vision)+len(mc.Image)+len(mc.Audio)) > 0 {
		return mc
	}
	return domain.DefaultModelsConfig()
}

/* SaveModelsConfig 落盘模型四槽。 */
func SaveModelsConfig(fsys osfs.OS, m domain.ModelsConfig) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(context.Background(), "models.json", data)
}

/* LoadSettings 读 settings.json，缺失回落默认值。 */
func LoadSettings(fsys osfs.OS) domain.Settings {
	out := domain.DefaultSettings()
	data, err := fsys.Read(context.Background(), "settings.json")
	if err != nil {
		return out
	}
	var s struct {
		SystemExtra    string   `json:"systemExtra"`
		TrimPercent    *int     `json:"trimPercent"` // 指针：区分未提交与显式 0（禁用）
		WorkDir        string   `json:"workDir"`
		CloseToTray    *bool    `json:"closeToTray"`
		DisabledSkills []string `json:"disabledSkills"`
		MaxIterations  *int     `json:"maxIterations"`
		DebugMode      *bool    `json:"debugMode"`
	}
	if json.Unmarshal(data, &s) != nil {
		return out
	}
	out.SystemExtra = s.SystemExtra
	out.WorkDir = s.WorkDir
	if s.CloseToTray != nil {
		out.CloseToTray = *s.CloseToTray
	}
	if s.TrimPercent != nil {
		out.TrimPercent = clamp(*s.TrimPercent, 0, 100)
	}
	out.DisabledSkills = s.DisabledSkills
	if s.MaxIterations != nil {
		out.MaxIterations = clamp(*s.MaxIterations, 0, 128)
	}
	if s.DebugMode != nil {
		out.DebugMode = *s.DebugMode
	}
	return out
}

/* SaveSettings 落盘行为设置。 */
func SaveSettings(fsys osfs.OS, s domain.Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(context.Background(), "settings.json", data)
}

/* LoadToolRules 读 toolRules.json；缺失或空档回落内置默认。 */
func LoadToolRules(fsys osfs.OS) []domain.ToolRule {
	data, err := fsys.Read(context.Background(), "toolRules.json")
	if err != nil {
		return domain.DefaultToolRules()
	}
	var rules []domain.ToolRule
	if json.Unmarshal(data, &rules) != nil || len(rules) == 0 {
		return domain.DefaultToolRules()
	}
	return rules
}

/* SaveToolRules 落盘审批策略。 */
func SaveToolRules(fsys osfs.OS, rules []domain.ToolRule) error {
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(context.Background(), "toolRules.json", data)
}

/* clamp 限值到 [lo, hi]。 */
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
