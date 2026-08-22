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

/*
ModelEntry 是一个模型条目；ModelsConfig 是模型四槽记录（models.json）：
main（主模型，驱动 agent 循环）/ vision（主模型无视觉时图片转写兜底）/
image（文生图）/ audio（语音合成）——后两者是主模型按需调用的工具后端。
每槽至多一条 Enabled。结构随后续数据建模演进。
*/
type ModelEntry struct {
	Name    string  `json:"name"`    // 模型名（provider 侧 ID）
	BaseURL string  `json:"baseUrl"`
	APIKey  string  `json:"apiKey"`
	Enabled bool    `json:"enabled"` // 每槽至多一条启用
	Tokens  int     `json:"tokens"`  // 累计用量（prompt+completion）
	Cost    float64 `json:"cost"`    // 累计花费（单价表后续接入）
}

type ModelsConfig struct {
	Main   []ModelEntry `json:"main"`
	Vision []ModelEntry `json:"vision"`
	Image  []ModelEntry `json:"image"`
	Audio  []ModelEntry `json:"audio"`
}

/* DefaultModelsConfig 给出出厂值（main 一条默认，apiKey 空零配置可启动）。 */
func DefaultModelsConfig() ModelsConfig {
	return ModelsConfig{
		Main: []ModelEntry{{
			Name:    "deepseek-ai/DeepSeek-V3.2",
			BaseURL: "https://api.siliconflow.cn/v1",
		}},
	}
}

/* LoadModelsConfig 读 models.json；兼容旧扁平结构（apiKey/model/baseUrl）
并迁移为 main 槽单条目（原文件备份 .bak）。 */
func LoadModelsConfig(fsys fs.FileSystem) ModelsConfig {
	out := DefaultModelsConfig()
	data, err := fsys.Read(context.Background(), "models.json")
	if err != nil {
		return out
	}
	var mc ModelsConfig
	if json.Unmarshal(data, &mc) == nil && (len(mc.Main)+len(mc.Vision)+len(mc.Image)+len(mc.Audio)) > 0 {
		return mc
	}
	/* 旧扁平结构迁移 */
	var old struct {
		APIKey  string `json:"apiKey"`
		Model   string `json:"model"`
		BaseURL string `json:"baseUrl"`
	}
	if json.Unmarshal(data, &old) == nil && old.Model != "" {
		e := ModelEntry{Name: old.Model, BaseURL: old.BaseURL, APIKey: old.APIKey, Enabled: true}
		if e.BaseURL == "" {
			e.BaseURL = out.Main[0].BaseURL
		}
		out.Main = []ModelEntry{e}
		_ = fsys.Write(context.Background(), "models.json.bak", data)
		_ = SaveModelsConfig(fsys, out)
	}
	return out
}

/* SaveModelsConfig 落盘模型四槽。 */
func SaveModelsConfig(fsys fs.FileSystem, m ModelsConfig) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(context.Background(), "models.json", data)
}

/* ActiveMain 返回主模型槽的生效条目（enabled 优先，否则首条；空槽 nil）。 */
func (m ModelsConfig) ActiveMain() *ModelEntry {
	for i := range m.Main {
		if m.Main[i].Enabled {
			return &m.Main[i]
		}
	}
	if len(m.Main) > 0 {
		return &m.Main[0]
	}
	return nil
}

/* Settings 是可热更的行为设置。
上下文机制（压缩/轮换/卸载）暂不装配——用户后续专门设计，字段不再保留。 */
type Settings struct {
	SystemExtra string     `json:"systemExtra"`
	ToolRules   []ToolRule `json:"toolRules"` // 审批策略（空 = 内置默认）
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
		{Tool: "terminal", Level: LevelWhite, List: []string{
			"ls", "cat", "head", "tail", "pwd", // POSIX 只读
			"dir", "type", "cd", "ver", // cmd 只读（Windows 原生 shell）
			"git status", "git diff", "git log", "go test",
		}},
		{Tool: "task", Level: LevelAsk},
		{Tool: "save_app", Level: LevelAsk},
		{Tool: "mcp.*", Level: LevelAsk},
	}
}

/* DefaultSettings 给出出厂值。 */
func DefaultSettings() Settings {
	return Settings{
		SystemExtra: "",
		ToolRules:   DefaultToolRules(),
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
	if len(s.ToolRules) > 0 {
		for i := range s.ToolRules {
			if s.ToolRules[i].Tool == "bash" {
				s.ToolRules[i].Tool = "terminal" // 旧存档里的工具名
			}
		}
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
