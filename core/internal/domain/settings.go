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
	Name          string            `json:"name"` // 模型名（provider 侧 ID）
	BaseURL       string            `json:"baseUrl"`
	APIKey        string            `json:"apiKey"`
	Headers       map[string]string `json:"headers,omitempty"`       // 自定义请求头（网关鉴权、组织 ID 等）
	Enabled       bool              `json:"enabled"`                 // 每槽至多一条启用
	Vision        bool              `json:"vision,omitempty"`        // 支持多模态视觉输入；false 时带图请求主动剥图（防 VLM 400 卡死会话）
	InTokens      int               `json:"inTokens,omitempty"`      // 累计输入 tokens（含缓存命中，对齐 openai 口径）
	OutTokens     int               `json:"outTokens,omitempty"`     // 累计输出 tokens
	CacheTokens   int               `json:"cacheTokens,omitempty"`   // 累计缓存命中 tokens（输入子集）
	Cost          float64           `json:"cost"`                    // 累计花费（单价表后续接入）
	ContextWindow int               `json:"contextWindow,omitempty"` // 上下文窗口（tokens，水位与压缩推荐用；0 未知)
	Protocol      string            `json:"protocol,omitempty"`      // API 协议：openai（默认）| responses | anthropic
}

/* 协议类型常量（Protocol 字段取值；空串按 openai 处理）。 */
const (
	ProtocolOpenAI    = "openai"    // OpenAI Chat Completions（/chat/completions）
	ProtocolResponses = "responses" // OpenAI Responses 格式（/responses，DeepSeek/Codex 等）
	ProtocolAnthropic = "anthropic" // Anthropic Messages（/v1/messages）
)

type ModelsConfig struct {
	Main   []ModelEntry `json:"main"`
	Vision []ModelEntry `json:"vision"`
	Image  []ModelEntry `json:"image"`
	Audio  []ModelEntry `json:"audio"`
}

/* DefaultModelsConfig 给出出厂值（main 一条默认并启用，apiKey 空零配置可启动）。 */
func DefaultModelsConfig() ModelsConfig {
	return ModelsConfig{
		Main: []ModelEntry{{
			Name:          "deepseek-ai/DeepSeek-V3.2",
			BaseURL:       "https://api.siliconflow.cn/v1",
			Enabled:       true,
			ContextWindow: 128000,
		}},
	}
}

/* LoadModelsConfig 读 models.json，缺失或损坏回落默认值。 */
func LoadModelsConfig(fsys fs.FileSystem) ModelsConfig {
	data, err := fsys.Read(context.Background(), "models.json")
	if err != nil {
		return DefaultModelsConfig()
	}
	var mc ModelsConfig
	if json.Unmarshal(data, &mc) == nil && (len(mc.Main)+len(mc.Vision)+len(mc.Image)+len(mc.Audio)) > 0 {
		return mc
	}
	return DefaultModelsConfig()
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

/* Settings 是可热更的行为设置（审批策略独立于 toolRules.json）。 */
type Settings struct {
	SystemExtra    string   `json:"systemExtra"`
	TrimPercent    int      `json:"trimPercent"`    // 上下文整理水位（模型窗口百分比，0=禁用自动整理）
	WorkDir        string   `json:"workDir"`        // 工作目录（terminal 默认目录；空=数据目录下 workspace/，相对=相对数据目录）
	CloseToTray    bool     `json:"closeToTray"`    // 桌面端点关闭 = 最小化到托盘（关窗时实时读取，即改即生效）
	DisabledSkills []string `json:"disabledSkills"` // 已禁用 skill 的目录名（load_skill/状态面板实时读取，system 清单下个 session 生效）
	MaxIterations  int      `json:"maxIterations"`  // 单轮对话的最大模型迭代次数（0 = 默认 12；随 Reassemble 生效）
}

/*
	DefaultSettings 给出出厂值（水位 75%：窗口自适应，留足摘要提前量；

点关闭默认弹窗询问退出，选「最小化到托盘」后即常驻托盘）。
*/
func DefaultSettings() Settings {
	return Settings{
		SystemExtra: "",
		TrimPercent: 75,
		CloseToTray: false,
	}
}

/* LoadSettings 读 settings.json，缺失回落默认值。 */
func LoadSettings(fsys fs.FileSystem) Settings {
	out := DefaultSettings()
	data, err := fsys.Read(context.Background(), "settings.json")
	if err != nil {
		return out
	}
	var s struct {
		SystemExtra string `json:"systemExtra"`
		TrimPercent *int   `json:"trimPercent"` // 指针：区分未提交与显式 0（禁用）
		WorkDir     string `json:"workDir"`
		CloseToTray *bool  `json:"closeToTray"`
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
	return out
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

/* SaveSettings 落盘行为设置。 */
func SaveSettings(fsys fs.FileSystem, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(context.Background(), "settings.json", data)
}
