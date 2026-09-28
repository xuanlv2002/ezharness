/* 配置记录（models/settings/toolRules）的加载与落盘用例。 */
package service

import (
	"context"
	"encoding/json"
	"log"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
	"ezharness/core/internal/osfs"
)

/* BootstrapHub 装配领域根：加载配置与索引并恢复活动会话。 */
func BootstrapHub(h *domain.Hub) {
	ctx := context.Background()
	if _, err := h.Fsys.Read(ctx, "models.json"); err != nil {
		h.Models = domain.DefaultModelsConfig()
		_ = SaveModelsConfig(ctx, h.Fsys, h.Models)
	} else {
		h.Models = LoadModelsConfig(ctx, h.Fsys)
	}
	if _, err := h.Fsys.Read(ctx, "settings.json"); err != nil {
		h.Settings = domain.DefaultSettings()
		_ = SaveSettings(ctx, h.Fsys, h.Settings)
	} else {
		h.Settings = LoadSettings(ctx, h.Fsys)
	}
	if _, err := h.Fsys.Read(ctx, "toolRules.json"); err != nil {
		h.ToolRules = domain.DefaultToolRules()
		_ = SaveToolRules(ctx, h.Fsys, h.ToolRules)
	} else {
		h.ToolRules = LoadToolRules(ctx, h.Fsys)
	}
	h.Stats = domain.NewStats(h.Fsys)
	h.Topics = hooks.NewTopics(h.Fsys)
	h.SetActive(h.BootstrapActive())
}

/* recordUsage 累计主模型用量并落盘（失败记日志不中断轮）。 */
func recordUsage(h *domain.Hub, u *types.Usage) {
	if h.ApplyUsage(u) {
		if err := SaveModelsConfig(context.Background(), h.Fsys, h.ModelsSnapshot()); err != nil {
			log.Printf("模型用量落盘失败: %v", err)
		}
	}
}

/* LoadModelsConfig 读 models.json，缺失或损坏回落默认值。 */
func LoadModelsConfig(ctx context.Context, fsys osfs.OS) domain.ModelsConfig {
	data, err := fsys.Read(ctx, "models.json")
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
func SaveModelsConfig(ctx context.Context, fsys osfs.OS, m domain.ModelsConfig) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(ctx, "models.json", data)
}

/* LoadSettings 读 settings.json，缺失回落默认值。 */
func LoadSettings(ctx context.Context, fsys osfs.OS) domain.Settings {
	out := domain.DefaultSettings()
	data, err := fsys.Read(ctx, "settings.json")
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
func SaveSettings(ctx context.Context, fsys osfs.OS, s domain.Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(ctx, "settings.json", data)
}

/* LoadToolRules 读 toolRules.json；缺失或空档回落内置默认。 */
func LoadToolRules(ctx context.Context, fsys osfs.OS) []domain.ToolRule {
	data, err := fsys.Read(ctx, "toolRules.json")
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
func SaveToolRules(ctx context.Context, fsys osfs.OS, rules []domain.ToolRule) error {
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(ctx, "toolRules.json", data)
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
