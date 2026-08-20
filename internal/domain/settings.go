/*
配置记录（数据目录）：
  - ModelConfig（models.json）：模型端点与凭证，与 mcp.json 对称，
    结构随数据建模独立演进。
  - Settings（settings.json）：harness 行为设置——系统提示追加、
    话题轮换水位、shell。
持久化到工作目录（进程 cwd 即数据目录）。
*/
package domain

import (
	"context"
	"encoding/json"

	"github.com/xuanlv2002/ezloop/ext/fs"
)

/* ModelConfig 是模型配置记录（models.json）。apiKey 为空合法——应用
照常启动，发消息时才提示未配置。 */
type ModelConfig struct {
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
	BaseURL string `json:"baseUrl"`
}

/* DefaultModelConfig 给出出厂值（apiKey 空，零配置可启动）。 */
func DefaultModelConfig() ModelConfig {
	return ModelConfig{
		APIKey:  "",
		Model:   "deepseek-ai/DeepSeek-V3.2",
		BaseURL: "https://api.siliconflow.cn/v1",
	}
}

/* LoadModelConfig 读 models.json，缺失或字段为空回落默认。 */
func LoadModelConfig(fsys fs.FileSystem) ModelConfig {
	out := DefaultModelConfig()
	data, err := fsys.Read(context.Background(), "models.json")
	if err != nil {
		return out
	}
	var m ModelConfig
	if json.Unmarshal(data, &m) != nil {
		return out
	}
	if m.Model != "" {
		out.Model = m.Model
	}
	if m.BaseURL != "" {
		out.BaseURL = m.BaseURL
	}
	out.APIKey = m.APIKey
	return out
}

/* SaveModelConfig 落盘模型配置。 */
func SaveModelConfig(fsys fs.FileSystem, m ModelConfig) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(context.Background(), "models.json", data)
}

/* Settings 是可热更的行为设置。 */
type Settings struct {
	SystemExtra     string     `json:"systemExtra"`
	RotateThreshold int        `json:"rotateThreshold"` // prompt tokens，<=0 禁用自动轮换
	Shell           string     `json:"shell"`           // "" / "auto" / "bash" / "pwsh" / "cmd"
	ToolRules       []ToolRule `json:"toolRules"`       // 审批策略（空 = 内置默认）
}

/* Level 是审批策略档位。 */
type Level string

const (
	LevelAsk   Level = "ask"   // 每次审批
	LevelBlack Level = "black" // 黑名单审批：名单外放行
	LevelWhite Level = "white" // 白名单免审：名单内放行
	LevelAuto  Level = "auto"  // 全部免审
)

/* ToolRule 是单个工具的审批策略；list 语义随档位（黑=命中才审，白=命中即免）。 */
type ToolRule struct {
	Tool  string   `json:"tool"`
	Level Level    `json:"level"`
	List  []string `json:"list"`
}

/* DefaultToolRules 内置默认（等价旧 needsApprove 硬编码语义）。 */
func DefaultToolRules() []ToolRule {
	return []ToolRule{
		{Tool: "read_file", Level: LevelAuto},
		{Tool: "write_file", Level: LevelAsk},
		{Tool: "edit_file", Level: LevelAsk},
		{Tool: "bash", Level: LevelWhite, List: []string{"ls", "cat", "head", "tail", "pwd", "git status", "git diff", "git log", "go test"}},
		{Tool: "task", Level: LevelAsk},
		{Tool: "mcp.*", Level: LevelAsk},
	}
}

/* DefaultSettings 给出出厂值。 */
func DefaultSettings() Settings {
	return Settings{
		SystemExtra:     "",
		RotateThreshold: 80000,
		Shell:           "auto",
		ToolRules:       DefaultToolRules(),
	}
}

/* LoadSettings 读 settings.json，缺失回落默认值。 */
func LoadSettings(fsys fs.FileSystem) Settings {
	out := DefaultSettings()
	data, err := fsys.Read(context.Background(), "settings.json")
	if err != nil {
		return out
	}
	var s Settings
	if json.Unmarshal(data, &s) != nil {
		return out
	}
	out.SystemExtra = s.SystemExtra
	out.RotateThreshold = s.RotateThreshold // 0 = 显式禁用轮换，不回落
	if s.Shell != "" {
		out.Shell = s.Shell
	}
	if len(s.ToolRules) > 0 {
		out.ToolRules = s.ToolRules
	}
	return out
}

/* SaveSettings 落盘行为设置。 */
func SaveSettings(fsys fs.FileSystem, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(context.Background(), "settings.json", data)
}
