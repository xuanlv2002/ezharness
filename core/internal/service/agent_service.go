/*
AgentService 负责 agent 的装配与重建：provider、hooks、warp、工具集。
文件与终端工具复用 ezloop 的 filetools hook（原生 shell，Windows 为 cmd，
模型适配环境），ezharness 只增补 save_app。

上下文机制：system 由 sysprompt hook 每轮注入（session 创建时组装一次：
人格+SystemExtra+长期记忆+skill/mcp 列表；重启从快照还原不重组）。
contextfix 修理残缺历史，offload 卸载大工具结果；压缩（compact）、
系统提醒（remind）、调用链（trace）见 internal/hooks 各文件。
*/
package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"ezharness/core/internal/config"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/ext/hook/contextfix"
	"github.com/xuanlv2002/ezloop/ext/hook/filetools"
	"github.com/xuanlv2002/ezloop/ext/hook/mcp"
	"github.com/xuanlv2002/ezloop/ext/hook/offload"
	"github.com/xuanlv2002/ezloop/ext/hook/skill"
	"github.com/xuanlv2002/ezloop/ext/hook/skilltool"
	"github.com/xuanlv2002/ezloop/ext/hook/task"
	"github.com/xuanlv2002/ezloop/ext/provider/anthropic"
	"github.com/xuanlv2002/ezloop/ext/provider/openai"
	"github.com/xuanlv2002/ezloop/ext/provider/openairesponses"
	"github.com/xuanlv2002/ezloop/ext/warp/model/modelretry"
	"github.com/xuanlv2002/ezloop/ext/warp/tool/limit"
	"github.com/xuanlv2002/ezloop/ext/warp/tool/safetool"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
	"github.com/xuanlv2002/ezloop/warp"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
	"ezharness/core/internal/osfs"
	"ezharness/core/internal/tools"
	"ezharness/core/internal/warp/modeldump"
	"ezharness/core/internal/warp/noempty"
	"ezharness/core/internal/warp/toolarg"
	"ezharness/core/internal/warp/visionguard"
)

/* AgentService 装配领域会话的运行时。 */
type AgentService struct {
	Hub       *domain.Hub
	Term      *TerminalService // 共享终端（魔法看板），可空：term_* 工具与状态注入的前提
	Browser   *BrowserService  // 共享浏览器桥（魔法看板），可空：browser_* 工具注入的前提
	McpRouter *mcp.Router      // 系统级 MCP router（全局单例注入），可空：不装配 mcp hook
}

/*
buildProvider 按条目协议构造模型 provider：openai（/chat/completions，

默认）、responses（OpenAI Responses 格式，DeepSeek/Codex 等）、
anthropic（Claude Messages）。三协议 Options 字段对齐（BaseURL/
APIKey/Model/Headers），anthropic 额外下发 max_tokens 默认值。
*/
func buildProvider(m *domain.ModelEntry) provider.ModelProvider {
	opts := openai.Options{
		BaseURL: m.BaseURL,
		APIKey:  m.APIKey,
		Headers: m.Headers,
		Model:   m.Name,
	}
	switch m.Protocol {
	case domain.ProtocolResponses:
		return openairesponses.New(openairesponses.Options{
			BaseURL: m.BaseURL, APIKey: m.APIKey, Headers: m.Headers, Model: m.Name,
		})
	case domain.ProtocolAnthropic:
		return anthropic.New(anthropic.Options{
			BaseURL: m.BaseURL, APIKey: m.APIKey, Headers: m.Headers, Model: m.Name,
		})
	default:
		return openai.New(opts)
	}
}

/* Assemble 按配置装配 agent 并注入会话（主模型取 models 四槽 main 启用条目）。 */
func (a *AgentService) Assemble(s *domain.Session, st domain.Settings) {
	ctx := context.Background()
	maxIters := 64 // 单轮最大模型迭代次数（设置页可配；0/负数回落默认 64，上限 128）
	if st.MaxIterations > 0 {
		maxIters = st.MaxIterations
	}
	if maxIters > 128 {
		maxIters = 128
	}

	main := a.Hub.ModelsSnapshot().ActiveMain()
	if main == nil {
		main = &domain.ModelEntry{}
	}
	provider := buildProvider(main)

	// system 两段式：每 session 固定——会话已有 SysPrompt 直接复用（compact
	// 热更过的状态是本 session 的真相，重建不得回退到快照）；恢复的会话从
	// 快照还原（记忆/skill/mcp 变更等下个 session），新会话组装一次后固定，
	// 直到 compact 创建新 session。
	var sys *hooks.SysPrompt
	if sp := s.SysPromptRef(); sp != nil {
		sys = sp
	} else if snap := s.Snapshot(); snap != nil {
		sys = hooks.NewSysPrompt(snap.SystemBase, snap.SummaryBlock)
	} else {
		sys = hooks.NewSysPrompt(buildSystemBase(ctx, st, s.Fsys), "")
	}
	s.SetSysP(sys)
	sys.SetIdentityFn(func() string { return hooks.SessionIdentityBlock(s.ID) }) // 会话身份：ID+存档路径（trim 折叠后的回忆入口）
	s.Sess.BindSys(sys, main.Name)

	approver, approveCh := approve.New(a.needsApprove)
	asker, answerCh := askuser.New()

	window := main.ContextWindow
	if window <= 0 {
		window = 128000 // 条目未填窗口时的兜底
	}
	s.Sess.BindCtx(func() (int, int) { return s.CtxTokens(), window })
	disabledSkills := func() []string { return a.Hub.SettingsSnapshot().DisabledSkills }
	// 终端/浏览器是进程生命周期态(重启即失),不进 system 固定段,每轮快照现查
	termsBrief := func() []string {
		if a.Term == nil {
			return nil
		}
		var out []string
		for _, t := range a.Term.List() {
			if t.Exited {
				continue
			}
			label := t.Name
			if d := briefSeg(t.Desc, 60); d != "" {
				label += "（" + d + "）"
			}
			out = append(out, label+"["+t.ID+"]")
		}
		return out
	}
	tabsBrief := func() []string {
		if a.Browser == nil {
			return nil
		}
		return a.Browser.TabsBrief()
	}
	remindHook := hooks.NewRemind(s.Fsys, s.Sess,
		func() int { return s.CtxTokens() },
		window,
		func() []hooks.StatusMcp { return mcpStatusList(s.Fsys) },
		disabledSkills,
		termsBrief,
		tabsBrief,
	)
	traceHook := hooks.NewTrace(s.Fsys, s.Sess, func() string { return main.Name })
	trimHook := hooks.NewTrim(provider, traceHook,
		window*st.TrimPercent/100, // 水位=窗口百分比，随模型自适应（换模型 Reassemble 重算）
		window,                    // 模型窗口（整理提示展示水位比例用）
		s.Fsys, func() string { return s.ID }, // 进度档案 progress.md 落盘
	)

	// 能力槽启用态：图片识别槽启用 → agent 获得图片识别工具
	visionOn := visionModel(a.Hub) != nil

	// 主模型视觉能力实时判断（换模型 Reassemble 后随设置即时生效）
	mainVision := func() bool {
		m := a.Hub.ModelsSnapshot().ActiveMain()
		return m != nil && m.Vision
	}
	// read_file 图片分支裁决：开视觉 → 走标记→OnLoop 转图片消息（持久化）；
	// 未开 → 引导 image_recognize（识别槽也未开则引导设置）
	readImage := func(path, _ string) (string, bool) {
		if mainVision() {
			return "", true
		}
		if visionOn {
			return fmt.Sprintf("[当前模型无多模态能力，无法读取图片 %s；可调用 image_recognize 工具识别]", path), false
		}
		return fmt.Sprintf("[当前模型无多模态能力，无法读取图片 %s；如需识别图片请在设置·模型启用图片识别槽]", path), false
	}

	// visionguard 最内层（紧贴 provider）：无视觉模型每次实际请求（含 retry）
	// 剥历史图片消息（请求视图，落盘不动，换回多模态自动恢复）
	modelWarps := []warp.ModelHandler{modeldump.Warp(), modelretry.Warp(), noempty.Warp(), visionguard.Warp(mainVision)}
	agentTools := append(tools.SaveApp(s.Fsys), tools.SharedTerm(a.Term)...)
	agentTools = append(agentTools, tools.SharedBrowser(a.Browser)...)
	if visionOn {
		agentTools = append(agentTools, tools.ImageRecognize(a)...)
	}
	if a.Browser != nil {
		a.Browser.SetVisionProbe(mainVision) // 截图返回形态随主模型视觉能力实时裁决
	}
	toolNames := []string{
		"read_file", "write_file", "edit_file", "terminal", "save_app",
		askuser.ToolName, task.ToolName,
		"mcp_router", hooks.TrimTool, skilltool.ToolName,
		"term_start", "term_send", "term_read", "term_list", "term_close",
		"browser_tab", "browser_action", "browser_read",
	}
	if visionOn {
		toolNames = append(toolNames, tools.ImageRecognizeTool)
	}
	agent := core.NewAgent(provider,
		core.WithModelWarp(modelWarps...),
		core.WithToolWarp(toolarg.Warp(s.Fsys), limit.Warp(4), safetool.Warp()),
		core.WithTools(agentTools...),
		core.WithHooks(
			sys, // startHooks 首位：system base 唯一来源；后续 hook 在其 OnStart 里追加 tool-guide 说明段
			traceHook, // toolStart 首位：task/ask_user/load_skill 等 OnToolStart 内干活的 hook 返回 Skip 会短路后续 hook，观测层必须排在它们前面才有 span
			contextfix.New(),
			filetools.New(s.Fsys, filetools.WithWorkDir(ResolveWorkDir(st.WorkDir)), filetools.WithImageHandler(readImage)),
			skilltool.New(s.Fsys, hooks.SkillsDir, disabledSkills),
			remindHook,         // 系统提醒：变更段插 <resource_change>? + 快照段插 agent_status；OnEnd 收尾 <end_reason>
			hooks.NewRefFile(), // 有引用轮次在输入前插 <reference_file> 结构化告知（附件+文件页标注统一，模型按需 read_file）
			approver,
			asker,
			task.New(),
			NewMcpHook(s.Fsys, a.McpRouter),
			offload.New(s.Fsys, offload.WithSkip(askuser.ToolName, task.ToolName, skilltool.ToolName), offload.WithReplayTool("read_file")), // load_skill 返回的指令集是后续行动依据,卸载再回读纯浪费
			hooks.NewGuard(s.Fsys, window), // 窗口余量兜底：offload 豁免名单（read_file 等）的大结果放不下时卸载，须在 offload 之后
			trimHook, // OnLoop 回边水位整理（就地截断，立即生效），OnToolStart 拦模型主动整理
			s.Sess,   // 最后落盘
		),
		core.WithLoopParams(core.LoopParams{MaxIterations: maxIters}),
		core.WithStreaming(true),
	)

	s.Attach(domain.Wiring{
		Agent:     agent,
		Provider:  provider,
		ApproveCh: approveCh,
		AnswerCh:  answerCh,
		Trace:     traceHook,
		ToolNames: toolNames,
	})
}

/* visionModel 返回图片识别槽的启用条目（无则 nil）。 */
func visionModel(h *domain.Hub) *domain.ModelEntry {
	for i := range h.ModelsSnapshot().Vision {
		if h.ModelsSnapshot().Vision[i].Enabled {
			return &h.ModelsSnapshot().Vision[i]
		}
	}
	return nil
}

/*
RecognizeImage 用图片识别槽模型识别一张图片（image_recognize 工具的
后端）。question 为识别侧重点（空 = 通用描述），由调用方按任务语境给定。
槽模型与启用态每次实时读取——设置变更即生效，无需重建 agent。
*/
func (a *AgentService) RecognizeImage(ctx context.Context, path, question string) (string, error) {
	m := visionModel(a.Hub)
	if m == nil || m.APIKey == "" {
		return "", errors.New("图片识别模型未启用（设置·模型·图片识别）")
	}
	data, err := a.Hub.Fsys.Read(ctx, path)
	if err != nil {
		return "", fmt.Errorf("读取图片失败: %w", err)
	}
	prompt := question
	if prompt == "" {
		prompt = "识别这张图片的内容：先概述是什么，再按需提取其中的文字、数据、代码或关键细节。"
	}
	prompt += "\n输出将直接交给另一个 agent 使用，请客观、结构化，不要寒暄。"
	// 按扩展名推图片 MIME（识别模型通用要求 image/*）
	mime := "image/jpeg"
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		mime = "image/png"
	case ".webp":
		mime = "image/webp"
	case ".gif":
		mime = "image/gif"
	}
	prov := buildProvider(m)
	resp, err := prov.Invoke(ctx, &types.ModelRequest{Messages: []types.Message{{
		Role:    types.RoleUser,
		Content: prompt,
		Images: []types.ImagePart{{
			MimeType: mime,
			Data:     base64.StdEncoding.EncodeToString(data),
		}},
	}}})
	if err != nil {
		return "", err
	}
	a.Hub.RecordVisionUsage(&resp.Usage)
	return resp.Content, nil
}

/*
	Reassemble 重建全部存活分支的 agent（配置变更后；运行中的分支

跳过——保留旧 wiring 到其轮结束，下次变更追平）。活动分支 busy
仍返回 ErrBusy 保持前端提示语义。
*/
func (a *AgentService) Reassemble(st domain.Settings) error {
	if a.Hub.Active.Busy() {
		return domain.ErrBusy
	}
	for _, s := range a.Hub.Sessions() {
		if s.Busy() {
			continue
		}
		a.Assemble(s, st)
	}
	return nil
}

/*
	needsApprove 按审批策略判定（安全页四档）。

人机交互与内部工具恒免审；未知工具默认审批。运行时读设置快照，
改策略即时生效（无需重建 agent）。
*/
func (a *AgentService) needsApprove(c *types.ToolCall) bool {
	name := c.Name
	if name == "mcp_router" {
		// ezloop mcp 是单一 router 工具，二段式（action/server/tool 在 args）。
		// 发现类（mcp_list/tool_list）只读无副作用，四档下一律免审。
		var a struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(c.Args, &a)
		if a.Action == "mcp_list" || a.Action == "tool_list" {
			return false
		}
		name = "mcp.*" // tool_call 按 server.tool 名单走四档
	}
	if name == "browser_tab" {
		// 标签清单只读无副作用，四档下一律免审（open/close 才是管控面）
		var a struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(c.Args, &a)
		if a.Action == "list" {
			return false
		}
	}
	rules := a.Hub.ToolRulesSnapshot()
	var rule *domain.ToolRule
	for i := range rules {
		if rules[i].Tool == name {
			rule = &rules[i]
			break
		}
	}
	if rule == nil {
		return true
	}
	switch rule.Level {
	case domain.LevelAuto:
		return false
	case domain.LevelAsk:
		return true
	case domain.LevelWhite, domain.LevelBlack:
		// 名单档仅终端（命令）与 mcp（server.tool）有能力；其余工具视为需审批
		if !listCapable(name) {
			return true
		}
		hit := matchRuleList(rule.List, name, c.Args)
		if rule.Level == domain.LevelWhite {
			return !hit
		}
		return hit
	}
	return true
}

/* listCapable 名单匹配能力面：终端系匹配命令、mcp 匹配 server.tool，其余工具无名单概念。 */
func listCapable(tool string) bool {
	switch tool {
	case "terminal", "term_start", "term_send", "mcp.*":
		return true
	}
	return false
}

/*
	matchRuleList 判定工具调用是否命中名单：终端系匹配命令（相等或词

边界前缀）、mcp 匹配 server 或 server.tool（点边界）。
*/
func matchRuleList(list []string, ruleTool string, args json.RawMessage) bool {
	key := ""
	switch ruleTool {
	case "terminal", "term_start", "term_send": // 共享终端执行与独立进程命令共用命令词匹配
		var a struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(args, &a)
		key = a.Command
	case "mcp.*":
		var a struct {
			Server string `json:"server"`
			Tool   string `json:"tool"`
		}
		_ = json.Unmarshal(args, &a)
		key = a.Server
		if a.Tool != "" {
			key = a.Server + "." + a.Tool
		}
	}
	if key == "" {
		return false
	}
	for _, e := range list {
		if key == e {
			return true
		}
		if (ruleTool == "terminal" || ruleTool == "term_start" || ruleTool == "term_send") && strings.HasPrefix(key, e+" ") {
			return true // 命令词边界
		}
		if ruleTool == "mcp.*" && strings.HasPrefix(key, e+".") {
			return true // server 前缀放行整站（点边界：time 不误命中 timeX）
		}
	}
	return false
}

/*
ResolveWorkDir 把工作目录配置解析为绝对路径：空 = 数据目录下 workspace/
（模型草稿与命令产物落这里，不与 models.json/sessions/ 等数据文件混放），
相对 = 相对数据目录；目录不存在则创建（terminal 与共享终端的执行目录必须存在）。
*/
func ResolveWorkDir(spec string) string {
	wd, err := os.Getwd() // 进程 cwd 即数据目录（启动时 chdir）
	if err != nil {
		wd = "."
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = filepath.Join(wd, "workspace")
	} else if !filepath.IsAbs(spec) {
		spec = filepath.Join(wd, spec) // 相对路径按数据目录解析
	}
	abs, err := filepath.Abs(spec)
	if err != nil {
		return spec
	}
	_ = os.MkdirAll(abs, 0o755)
	return abs
}

/*
buildSystemBase 组装 session 的 system 基础段：人格 + SystemExtra +
标签化注入块（<memory> 长期记忆结构+索引 / <skills> 技能列表 /
<mcp> MCP 列表）。只在 session 创建时调用一次（同 session 不变）；
skill 全文与记忆细节不注入（模型按需用文件工具读取），列表变更要等
下个 session 才进 system，期间由 remind 变更段的 <resource_change> 告知模型。
*/
func buildSystemBase(ctx context.Context, st domain.Settings, fsys osfs.OS) string {
	var b strings.Builder
	b.WriteString("你是 ezharness——一个持续陪伴用户的设备级 agent，可全权操作本机文件与命令。" +
		"能用工具就用工具，回答简洁。" +
		"用户需要小工具或网页时用 save_app 生成为快应用，用户可一键启动。" +
		"重要的用户偏好与事实可写入长期记忆（结构见 <memory> 块）。" +
		"接任务先看 <action> 行动准则；给用户的可点击入口与输出格式遵守 <output>。")
	if st.SystemExtra != "" {
		b.WriteString("\n\n" + st.SystemExtra)
	}
	dataDir, _ := os.Getwd() // 进程 cwd 即数据目录（启动时 chdir）
	workDir := ResolveWorkDir(st.WorkDir)
	p := func(rel string) string { return filepath.ToSlash(filepath.Join(dataDir, rel)) }
	memRoot := p("memory")
	b.WriteString("\n\n<workspace>\n" +
		"# 工作区：三个可写位置（下列均为完整绝对路径，直接使用，不要自行拼接）\n" +
		"# " + p("workspace") + "   工作目录，草稿/脚本/命令产物一律放这里（terminal 默认执行目录：" + filepath.ToSlash(workDir) + "）；" +
		"其下 tmp/ 是用户上传附件的暂存处，需要附件内容时用 read_file 按路径读取（图片会作为图片消息进入你的上下文，无需调用识别工具）\n" +
		"# " + memRoot + "/longterm   长期记忆：harness.md 是索引（已注入上下文，见 <memory>），user/projects/lessons 三个固定主题文件按主题沉淀；会话归档时系统会自动合并更新\n" +
		"# " + memRoot + "/skills     技能库：每技能一个子目录（SKILL.md 指令 + scripts/ 脚本），新建后下个 session 进清单\n" +
		"# 行为规则（不需要记路径，按规则做即可）：\n" +
		"# - 技能清单以 <skills> 段、MCP 服务以 <mcp> 段为准，不要读目录或配置文件去发现它们；\n" +
		"# - 快应用由 save_app 工具生成与更新，不要手动改快应用目录；\n" +
		"# - 本会话的存档与进度档案路径见 <session> 块（回忆入口）；其他历史会话的存档不要主动翻阅，确有需要先问用户；\n" +
		"# - 超长工具结果会被系统自动卸载为文件并在工具结果里给出路径，按提示 read_file 取回，不要主动浏览卸载区；\n" +
		"# - 数据目录下的配置与索引文件（settings、models、stats、topics 等）由应用管理，不要改写；\n" +
		"# - terminal 每条命令是独立进程（cd 不跨命令保留）；所有文件读写与命令一律绝对路径，临时文件不要丢在工作目录外。\n" +
		"</workspace>")
	b.WriteString("\n\n<memory>\n" +
		"# 长期记忆\n" +
		"- 索引 " + memRoot + "/longterm/harness.md：长期记忆入口，全文见下方，可用文件工具直接更新\n" +
		"- 主题文件 " + memRoot + "/longterm/{user,projects,lessons}.md：用户偏好与事实 / 项目与任务背景 / 踩坑与经验，" +
		"不进上下文，需要时用 findstr/grep 检索；会话归档时系统会把值得长期保留的内容自动合并进这三个文件，对话中也可直接编辑\n" +
		"# 索引 harness.md 全文\n" +
		hooks.EnsureHarnessMd(ctx, fsys) +
		"\n</memory>")
	skills, err := skill.LoadDir(ctx, fsys, hooks.SkillsDir)
	if err == nil && len(st.DisabledSkills) > 0 {
		kept := skills[:0]
		for _, sk := range skills {
			if !slices.Contains(st.DisabledSkills, hooks.SkillDirOf(sk.Path)) {
				kept = append(kept, sk)
			}
		}
		skills = kept
	}
	if len(skills) > 0 {
		b.WriteString("\n\n<skills>\n（可用技能清单，以此为准，不要读取 memory/skills 目录来发现技能；" +
			"技能正文在 " + memRoot + "/skills/<名>/SKILL.md，可用文件工具编辑，改动下个 session 生效，" +
			"轮内变更见 <resource_change>；使用前先调用 load_skill 获取完整指令与脚本路径）")
		for _, sk := range skills {
			desc := sk.Description
			if desc == "" {
				desc = "（无描述）"
			}
			fmt.Fprintf(&b, "\n- %s: %s", sk.Name, desc)
		}
		b.WriteString("\n</skills>")
	}
	if lines := mcpListLines(fsys); len(lines) > 0 {
		b.WriteString("\n\n<mcp>\n（可用 MCP 服务清单，以此为准，不要读取 mcp.json 来发现服务；" +
			"经 mcp_router 工具调用，先用 tool_list 拉取某服务的工具清单再 tool_call，轮内变更见 <resource_change>）")
		for _, l := range lines {
			b.WriteString("\n" + l)
		}
		b.WriteString("\n（HTTP 直调：core 把上面的 MCP 服务整体暴露为本机 API，不经你中转，页面、脚本、外部程序都可调——\n" +
			"POST /api/mcp/call，JSON 载荷 {\"server\":\"服务名\",\"tool\":\"工具名\",\"args\":{参数}}，" +
			"响应 {\"result\":\"文本\"}（失败 {\"error\"}）；GET /api/mcp 返回服务清单。服务名/工具名以上方清单为准。\n" +
			"同源页面（如 save_app 快应用）直接用相对路径：fetch('/api/mcp/call', {method: 'POST', " +
			"headers: {'Content-Type': 'application/json'}, body: JSON.stringify({server: '服务名', tool: '工具名', args: {}})})，" +
			"即可把 MCP 工具当作页面后端。例：实时时钟快应用——setInterval 定时调 timeNow，把返回的 d.result 渲染到页面）")
		if base := localAPIBase(); base != "" {
			b.WriteString("\n（非同源客户端用完整地址：" + base + "/api/mcp/call（调用）/ " + base + "/api/mcp（清单））")
		}
		b.WriteString("\n</mcp>")
	}
	b.WriteString("\n\n<action>\n" +
		"# 行动准则\n" +
		"1. 接任务先匹配技能：对照 <skills> 清单，命中就 load_skill 加载后按其指引执行，不要绕过现成技能自己造流程；没命中再自行设计。\n" +
		"2. 动手前想清楚：多步骤或有风险的任务，先用 ask_user 提交计划（options 给候选，如 [\"执行\",\"否决\",\"修改\"]）请用户处置，获批后再执行；一步能完成的小事直接做。\n" +
		"3. 执行工具的选择：\n" +
		"- 本机一次性命令（构建、查询、跑脚本）用 terminal；\n" +
		"- 需要交互式应答、长驻程序、或希望用户实时看到过程时，用 term_* 共享终端（term_start 新建可带名称与首条命令，term_send 发命令并等输出静默返回（也用于应答交互或发中断信号），term_read 游标式续读，term_list 查全部含用户手开的；长驻程序运行中或任务收尾时把终端入口交付给用户）；\n" +
		"- 检索网页、查资料、操作网页用 browser_* 共享浏览器（真实 Chromium，用户实时共见可直接接管；browser_tab 开标签自动展开浏览器页，首次使用会自动下载 Chromium 需等待；browser_action 操作页面——优先 CSS 选择器，定位不了先截图按视口坐标；browser_read 读正文/链接/截图）；与 terminal（本机命令）互补。\n" +
		"4. MCP：内置工具够用就不绕道；用 MCP 时先 mcp_list / tool_list 发现能力再 tool_call，不凭记忆猜工具名和参数。\n" +
		"5. 可并行、相互独立、或会产生大量中间输出的子任务，交 task 分身执行（工具集相同、过程互不干扰，结果直接回传），主对话只接结论；任务描述必须自包含（分身看不到本轮对话之外的语境）：写清目标、输入、涉及的文件绝对路径与期望的返回格式；无依赖的子任务一次并行发多个。\n" +
		"6. 交付与连续性：任务收尾把成果入口用 <$supper_url> 交付（格式见 <output>），产物文件放工作目录；" +
		"上下文被整理后，从 <session> 块告知的进度档案恢复现场接着干，长任务到达阶段性节点时也可主动把进度补写进该档案。\n" +
		"</action>")
	b.WriteString("\n\n<output>\n" +
		"# 输出规范\n" +
		"## 可点击入口 <$supper_url>（格式错就不会渲染成可点击入口，用户只能看到原文，务必照抄格式）\n" +
		"规则：整段闭合包裹、标签紧贴地址、地址内不能有空格/换行/文字；说明文字写在标签外面。\n" +
		"正确示例：\n" +
		"<$supper_url>https://example.com/report</$supper_url>\n" +
		"<$supper_url>term://终端id</$supper_url>（id 来自 term_start/term_list）\n" +
		"<$supper_url>browser://标签id</$supper_url>（id 来自 browser_tab）\n" +
		"<$supper_url>app://快应用名</$supper_url>\n" +
		"<$supper_url>file://C:/完整/绝对/路径.md</$supper_url>\n" +
		"错误示例（不会渲染）：\n" +
		"<$supper_url>点这里看终端 term://t1</$supper_url>   ← 地址里夹了文字\n" +
		"<$supper_url>term:t_1</$supper_url>                 ← 缺 //\n" +
		"<$supper_url>https://example.com                    ← 没闭合\n" +
		"## 工具参数引用 <@toolArg>\n" +
		"工具参数的字符串值里写 <@toolArg>绝对路径</@toolArg>，执行时自动展开为该文件内容——" +
		"需要把已生成的文件全文作为参数喂给工具（如 save_app 的代码参数引用草稿文件）时用它，" +
		"省去先 read_file 再粘贴；单文件上限 200000 字符，读不到会报错。\n" +
		"## 回复风格\n" +
		"结论先行：先给结果与入口，再给必要说明；不逐条复述工具输出；" +
		"用户在终端/浏览器里看得见的过程不要文字直播；长说明用列表。\n" +
		"</output>")
	return b.String()
}

/* localAPIBase 返回本机 API 基址（供 <mcp> 段告知 HTTP 直调端点；读不到配置返回空）。 */
func localAPIBase() string {
	cfg, err := config.Load()
	if err != nil || cfg.Port <= 0 {
		return ""
	}
	return "http://127.0.0.1:" + strconv.Itoa(cfg.Port)
}

/* briefSeg 清单条目的补充段（终端 desc / 标签 title）压平截断，防伪造行与超长。 */
func briefSeg(s string, max int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

/* mcpListLines 返回启用 server 的"名: 描述"清单。 */
func mcpListLines(fsys osfs.OS) []string {
	f := loadMcpFileOrNil(fsys)
	if f == nil {
		return nil
	}
	var out []string
	for _, srv := range f.Servers {
		if srv.IsEnabled() {
			desc := srv.Description
			if desc == "" {
				desc = "（无描述）"
			}
			out = append(out, "- "+srv.Name+": "+desc)
		}
	}
	return out
}

/* mcpStatusList 返回 MCP 清单（remind 变更基线与 available 清单用）。 */
func mcpStatusList(fsys osfs.OS) []hooks.StatusMcp {
	f := loadMcpFileOrNil(fsys)
	if f == nil {
		return nil
	}
	out := make([]hooks.StatusMcp, 0, len(f.Servers))
	for _, srv := range f.Servers {
		if !srv.IsEnabled() {
			continue
		}
		out = append(out, hooks.StatusMcp{Name: srv.Name, Desc: srv.Description})
	}
	return out
}
