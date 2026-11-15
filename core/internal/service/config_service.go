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
	h.Media = LoadMediaConfig(ctx, h.Fsys)
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

/* recordUsage 累计该会话所用模型的用量并落盘（失败记日志不中断轮）。 */
func recordUsage(h *domain.Hub, model string, u *types.Usage) {
	if h.ApplyUsage(model, u) {
		if err := SaveModelsConfig(context.Background(), h.Fsys, h.ModelsSnapshot()); err != nil {
			log.Printf("模型用量落盘失败: %v", err)
		}
	}
}

/*
LoadModelsConfig 读 models.json，缺失或损坏回落默认值。
只认 main；旧版残留的 image/audio/video 字段直接忽略（生成模型只从
media.json 读取，不做迁移）。
*/
func LoadModelsConfig(ctx context.Context, fsys osfs.OS) domain.ModelsConfig {
	data, err := fsys.Read(ctx, "models.json")
	if err != nil {
		return domain.DefaultModelsConfig()
	}
	fsys.Restrict("models.json") // 含 apiKey：存量文件启动时收敛为仅属主可读写
	var mc domain.ModelsConfig
	if json.Unmarshal(data, &mc) == nil && len(mc.Main) > 0 {
		return mc
	}
	return domain.DefaultModelsConfig()
}

/* SaveModelsConfig 落盘语言模型槽。 */
func SaveModelsConfig(ctx context.Context, fsys osfs.OS, m domain.ModelsConfig) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsys.WritePrivate(ctx, "models.json", data)
}

const mediaConfigFile = "media.json"

/*
	LoadMediaConfig 读 media.json（多媒体生成模型：image/audio/video 三类），

缺失给空配置（三类都未配置是正常态）。
*/
func LoadMediaConfig(ctx context.Context, fsys osfs.OS) domain.MediaConfig {
	var mc domain.MediaConfig
	if data, err := fsys.Read(ctx, mediaConfigFile); err == nil {
		fsys.Restrict(mediaConfigFile)
		_ = json.Unmarshal(data, &mc)
	}
	return mc
}

/* SaveMediaConfig 落盘多媒体生成模型（含 apiKey，属主权限）。 */
func SaveMediaConfig(ctx context.Context, fsys osfs.OS, m domain.MediaConfig) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsys.WritePrivate(ctx, mediaConfigFile, data)
}

/* LoadSettings 读 settings.json，缺失回落默认值。 */
func LoadSettings(ctx context.Context, fsys osfs.OS) domain.Settings {
	out := domain.DefaultSettings()
	data, err := fsys.Read(ctx, "settings.json")
	if err != nil {
		return out
	}
	var s struct {
		SystemExtra      string   `json:"systemExtra"`
		TrimPercent      *int     `json:"trimPercent"` // 指针：区分未提交与显式 0（禁用）
		WorkDir          string   `json:"workDir"`
		CloseToTray      *bool    `json:"closeToTray"`
		PetEnabled       *bool    `json:"petEnabled"`
		DisabledSkills   []string `json:"disabledSkills"`
		MaxIterations    *int     `json:"maxIterations"`
		DebugMode        *bool    `json:"debugMode"`
		PinnedApps       []string `json:"pinnedApps"`
		OffloadThreshold *int     `json:"offloadThreshold"`
		GenQuotaImage    *int     `json:"genQuotaImage"`
		GenQuotaAudio    *int     `json:"genQuotaAudio"`
		GenQuotaVideo    *int     `json:"genQuotaVideo"`
	}
	if json.Unmarshal(data, &s) != nil {
		return out
	}
	out.SystemExtra = s.SystemExtra
	out.WorkDir = s.WorkDir
	if s.CloseToTray != nil {
		out.CloseToTray = *s.CloseToTray
	}
	if s.PetEnabled != nil {
		out.PetEnabled = *s.PetEnabled
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
	if s.OffloadThreshold != nil {
		out.OffloadThreshold = clamp(*s.OffloadThreshold, 0, 1048576)
	}
	if s.GenQuotaImage != nil {
		out.GenQuotaImage = clamp(*s.GenQuotaImage, 0, 1000)
	}
	if s.GenQuotaAudio != nil {
		out.GenQuotaAudio = clamp(*s.GenQuotaAudio, 0, 1000)
	}
	if s.GenQuotaVideo != nil {
		out.GenQuotaVideo = clamp(*s.GenQuotaVideo, 0, 1000)
	}
	out.PinnedApps = s.PinnedApps
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
