/* 配置记录的结构与默认值（加载与落盘在 service 层）。 */
package domain

/*
ModelEntry 是一个模型条目（语言模型与多媒体生成模型共用同一形态：
name + baseUrl + apiKey + headers）。ModelsConfig 是语言模型记录
（models.json 的 main 槽，驱动 agent 循环；支持视觉则图片直接进上下文）；
多媒体生成模型（文生图/语音合成/视频生成）在 MediaConfig（media.json），
两类彻底分离——语言模型不进生成清单，生成模型不进对话模型取值域。
每槽至多一条 Enabled。
*/
type ModelEntry struct {
	Name          string            `json:"name"` // 模型名（provider 侧 ID）
	BaseURL       string            `json:"baseUrl"`
	APIKey        string            `json:"apiKey"`
	Headers       map[string]string `json:"headers,omitempty"`       // 自定义请求头（网关鉴权、组织 ID 等）
	Enabled       bool              `json:"enabled"`                 // 每槽至多一条启用
	Disabled      bool              `json:"disabled,omitempty"`      // 关闭：不可被 model_call/task 选用,也不进可用模型清单
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
	Main []ModelEntry `json:"main"`
}

/*
MediaConfig 是多媒体生成模型记录（media.json）：三类能力各自独立列表，
与语言模型（models.json）分离。SiliconFlow 兼容端点（baseUrl 填到 /v1）；
配置并启用对应类别后 agent 经统一的 media 工具（action=image/audio/video）
调用；未配置/关闭的能力调用返回「当前无能力」。
（贵价：默认每次审批 + 每日配额）。
*/
type MediaConfig struct {
	Image []ModelEntry `json:"image"`
	Audio []ModelEntry `json:"audio"`
	Video []ModelEntry `json:"video"`
}

/*
	ActiveSlot 返回列表的生效条目：仅认 Enabled 且未关闭——全部停用即整类

关闭（工具不注册）；main 槽的"必须有一个"兜底在 UpdateModels 里做。
*/
func ActiveSlot(entries []ModelEntry) *ModelEntry {
	for i := range entries {
		if entries[i].Enabled && !entries[i].Disabled {
			return &entries[i]
		}
	}
	return nil
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

/* ActiveMain 返回主模型槽的默认条目（enabled 优先且跳过已关闭,否则首条未关闭；全关/空槽 nil）。 */
func (m ModelsConfig) ActiveMain() *ModelEntry {
	for i := range m.Main {
		if m.Main[i].Enabled && !m.Main[i].Disabled {
			return &m.Main[i]
		}
	}
	for i := range m.Main {
		if !m.Main[i].Disabled {
			return &m.Main[i]
		}
	}
	return nil
}

/*
ResolveMain 解析会话的主模型：name 非空且存在且未关闭时用它；name 为空、
条目被删或已关闭时回落 ActiveMain（全局默认）。会话切换模型只写会话
偏好，不改 models.json 的 enabled 位。
*/
func (m ModelsConfig) ResolveMain(name string) *ModelEntry {
	if name != "" {
		for i := range m.Main {
			if m.Main[i].Name == name && !m.Main[i].Disabled {
				return &m.Main[i]
			}
		}
	}
	return m.ActiveMain()
}

/* Settings 是可热更的行为设置（审批策略独立于 toolRules.json）。 */
type Settings struct {
	SystemExtra      string   `json:"systemExtra"`
	TrimPercent      int      `json:"trimPercent"`          // 上下文整理水位（模型窗口百分比，0=禁用自动整理）
	WorkDir          string   `json:"workDir"`              // 工作目录（terminal 默认目录；空=数据目录下 workspace/，相对=相对数据目录）
	CloseToTray      bool     `json:"closeToTray"`          // 桌面端点关闭 = 最小化到托盘（关窗时实时读取，即改即生效）
	PetEnabled       bool     `json:"petEnabled"`           // 桌面悬浮球开关（desktop 壳启动时读取；关=不建窗，设置页可重开）
	DisabledSkills   []string `json:"disabledSkills"`       // 已禁用 skill 的目录名（load_skill/状态面板实时读取，system 清单下个 session 生效）
	MaxIterations    int      `json:"maxIterations"`        // 单轮对话的最大模型迭代次数（0 = 默认 64，上限 128；随 Reassemble 生效）
	DebugMode        bool     `json:"debugMode"`            // 调试模式：把每次模型请求的全量输入写进日志（modeldump 实时读取，即改即生效）
	PinnedApps       []string `json:"pinnedApps,omitempty"` // 钉到桌面小助手的快应用名（快应用页/宠物窗轮询读取，即改即生效）
	OffloadThreshold int      `json:"offloadThreshold"`     // 工具结果卸载阈值（字节；超过即卸载到文件只留摘要头，0=默认 4096；摘要头随阈值分档封顶 1KB；随 Reassemble 生效）
	/* 生成类模型（image/audio/video）每日调用配额（agent 工具与 OpenAPI 同一计数；
	0 = 用默认。这三类按次计费且单价高，配额是审批之外的第二道闸 */
	GenQuotaImage int `json:"genQuotaImage,omitempty"`
	GenQuotaAudio int `json:"genQuotaAudio,omitempty"`
	GenQuotaVideo int `json:"genQuotaVideo,omitempty"`
}

/* 生成类配额默认值（设置页可改；计数单位：image=张、audio/video=次）。 */
const (
	DefaultGenQuotaImage = 20
	DefaultGenQuotaAudio = 20
	DefaultGenQuotaVideo = 3
)

/* 生成类别（配额键、模型槽名、工具后缀共用）。 */
const (
	GenImage = "image"
	GenAudio = "audio"
	GenVideo = "video"
)

/* MediaJob 任务状态机取值。 */
const (
	JobQueued  = "queued"
	JobRunning = "running"
	JobDone    = "done"
	JobFailed  = "failed"
)

/*
GenFile 是一次生成类调用的产物（service 产出，agent 工具与 OpenAPI
响应共用视图）：磁盘是唯一真身，Path 给绝对正斜杠路径；图片/音频额外
内嵌 base64（快应用与脚本直接可用），视频只给 URL（体积大）。
*/
type GenFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int    `json:"size"`
	Mime string `json:"mime"`
	URL  string `json:"url,omitempty"`
	B64  string `json:"b64,omitempty"`
}

/* GenQuota 是配额用量视图（工具结果与 OpenAPI 响应附带，调用方见余量）。 */
type GenQuota struct {
	Kind  string `json:"kind"`
	Used  int    `json:"used"`
	Limit int    `json:"limit"`
}

/*
MediaJob 是一次异步生成任务（多媒体模块）：提交即返回，短等待未完成由
调用方持 id 轮询（agent 的 media 工具 action=status / OpenAPI 的 jobs 端点）。
状态机 queued → running → done|failed；文件产物在 done 时填充。任务
档案持久（mediajobs.json，封顶 200 条），core 重启时未完成的任务标记
失败并退还当日配额。后期事件回调（到点通知会话）在本结构上扩展。
*/
type MediaJob struct {
	ID        string    `json:"id"`               // <kind>-<hex6>，工具/接口的查询键
	Kind      string    `json:"kind"`             // image | audio | video
	Status    string    `json:"status"`           // queued | running | done | failed
	Model     string    `json:"model"`            // 使用的模型条目名
	Brief     string    `json:"brief"`            // 任务摘要（清单展示，截断的 prompt/input）
	CreatedAt int64     `json:"createdAt"`        // 提交时刻（UnixMilli）
	DoneAt    int64     `json:"doneAt,omitempty"` // 终态时刻
	Files     []GenFile `json:"files,omitempty"`  // 产物（done 时）
	Quota     GenQuota  `json:"quota"`            // 配额视图（提交时占额后的余量）
	Err       string    `json:"err,omitempty"`    // 失败原因（failed 时）

	Units int `json:"-"` // 配额计量单位（image=张数，audio/video=1；提交时记录，退还对账用）
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
		PetEnabled:  true,
	}
}
