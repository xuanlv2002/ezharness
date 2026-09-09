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
	"strings"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/ext/hook/contextfix"
	"github.com/xuanlv2002/ezloop/ext/hook/filetools"
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

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
	"ezharness/internal/osfs"
	"ezharness/internal/tools"
	"ezharness/internal/warp/modeldump"
	"ezharness/internal/warp/toolarg"
	"ezharness/internal/warp/visionguard"
)

/* AgentService 装配领域会话的运行时。 */
type AgentService struct {
	Hub  *domain.Hub
	Term *TerminalService // 共享终端（魔法看板），可空：term_* 工具与状态注入的前提
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

	main := a.Hub.ModelsSnapshot().ActiveMain()
	if main == nil {
		main = &domain.ModelEntry{}
	}
	provider := buildProvider(main)

	// system 两段式：每 session 固定——已有 SysPrompt 直接复用（Resume/
	// compact 热更过的状态是本 session 的真相，重建不得回退到旧快照）；
	// 恢复的会话从快照还原（记忆/skill/mcp 变更等下个 session），新会话
	// 组装一次后固定，直到 compact 创建新 session。
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
		window = 128000 // 旧 models.json 无 contextWindow 字段的兜底
	}
	s.Sess.BindCtx(func() (int, int) { return s.CtxTokens(), window })
	disabledSkills := func() []string { return a.Hub.SettingsSnapshot().DisabledSkills }
	remindHook := hooks.NewRemind(s.Fsys, s.Sess,
		func() int { return s.CtxTokens() },
		window,
		func() []hooks.StatusMcp { return mcpStatusList(s.Fsys) },
		termReportFn(a.Term),
		disabledSkills,
	)
	traceHook := hooks.NewTrace(s.Fsys, s.Sess, func() string { return main.Name })
	trimHook := hooks.NewTrim(provider, traceHook,
		window*st.TrimPercent/100, // 水位=窗口百分比，随模型自适应（换模型 Reassemble 重算）
		window, // 模型窗口（整理提示展示水位比例用）
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
	modelWarps := []warp.ModelHandler{modeldump.Warp(), modelretry.Warp(), visionguard.Warp(mainVision)}
	agentTools := append(tools.SaveApp(s.Fsys), tools.SharedTerm(a.Term)...)
	if visionOn {
		agentTools = append(agentTools, tools.ImageRecognize(a)...)
	}
	toolNames := []string{
		"read_file", "write_file", "edit_file", "terminal", "save_app",
		askuser.ToolName, task.ToolName,
		"mcp_router", hooks.TrimTool, skilltool.ToolName,
		"term_start", "term_send", "term_read", "term_list", "term_close",
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
			contextfix.New(),
			filetools.New(s.Fsys, filetools.WithWorkDir(ResolveWorkDir(st.WorkDir)), filetools.WithImageHandler(readImage)),
			skilltool.New(s.Fsys, hooks.SkillsDir, disabledSkills),
			remindHook, // 系统提醒：变更段插 <res_change>? + 快照段插 agent_status；OnEnd 收尾 <end_reason>
			hooks.NewUploadFile(), // 有附件轮次在输入前插 <upload_file> 路径告知（模型按需 read_file）
			approver,
			asker,
			task.New(),
			NewMcpHook(s.Fsys),
			offload.New(s.Fsys, offload.WithSkip(askuser.ToolName, task.ToolName, skilltool.ToolName), offload.WithReplayTool("read_file")), // load_skill 返回的指令集是后续行动依据,卸载再回读纯浪费
			hooks.NewGuard(s.Fsys, window), // 窗口余量兜底：offload 豁免名单（read_file 等）的大结果放不下时卸载，须在 offload 之后
			trimHook, // OnLoop 回边水位整理（就地截断，立即生效），OnToolStart 拦模型主动整理
			traceHook,
			s.Sess, // 最后落盘
		),
		core.WithLoopParams(core.LoopParams{MaxIterations: maxIters(st)}),
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

/* maxIters 单轮最大模型迭代次数（设置页可配；0/负数回落默认 12）。 */
func maxIters(st domain.Settings) int {
	if st.MaxIterations <= 0 {
		return 12
	}
	return st.MaxIterations
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
	prov := buildProvider(m)
	resp, err := prov.Invoke(ctx, &types.ModelRequest{Messages: []types.Message{{
		Role: types.RoleUser,
		Content: prompt,
		Images: []types.ImagePart{{
			MimeType: mimeOf(path),
			Data:     base64.StdEncoding.EncodeToString(data),
		}},
	}}})
	if err != nil {
		return "", err
	}
	a.Hub.RecordVisionUsage(&resp.Usage)
	return resp.Content, nil
}

/* mimeOf 按扩展名推图片 MIME（识别模型通用要求 image/*）。 */
func mimeOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	}
	return "image/jpeg"
}

/* Reassemble 重建全部存活分支的 agent（配置变更后；运行中的分支
跳过——保留旧 wiring 到其轮结束，下次变更追平）。活动分支 busy
仍返回 ErrBusy 保持前端提示语义。 */
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
	switch c.Name {
	case askuser.ToolName, skilltool.ToolName, hooks.TrimTool:
		return false // 交互与内部工具不属用户管控面（加载技能/整理上下文均为只读元操作）
	}
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
	case domain.LevelWhite:
		return !matchRuleList(rule.List, name, c.Args)
	case domain.LevelBlack:
		return matchRuleList(rule.List, name, c.Args)
	}
	return true
}

/*
	matchRuleList 判定工具调用是否命中名单：bash 匹配命令（相等或词边界前缀）、

文件工具匹配路径前缀、mcp 匹配 server 或 server.tool。
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
	case "read_file", "write_file", "edit_file":
		var a struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(args, &a)
		key = a.Path
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
		if ruleTool != "terminal" && ruleTool != "term_start" && ruleTool != "term_send" && ruleTool != "mcp.*" && strings.HasPrefix(key, e) {
			return true // 路径前缀
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
下个 session 才进 system，过渡期靠 remind 变更段的 <res_change> 告知模型。
*/
func buildSystemBase(ctx context.Context, st domain.Settings, fsys osfs.OS) string {
	var b strings.Builder
	b.WriteString("你是 ezharness——一个持续陪伴用户的设备级 agent，可全权操作本机文件与命令。" +
		"能用工具就用工具，回答简洁。" +
		"用户需要小工具或网页时用 save_app 生成为快应用，用户可一键启动。" +
		"重要的用户偏好与事实可写入长期记忆（结构见 <memory> 块）。")
	if st.SystemExtra != "" {
		b.WriteString("\n\n" + st.SystemExtra)
	}
	dataDir, _ := os.Getwd() // 进程 cwd 即数据目录（启动时 chdir）
	workDir := ResolveWorkDir(st.WorkDir)
	p := func(rel string) string { return filepath.ToSlash(filepath.Join(dataDir, rel)) }
	b.WriteString("\n\n<workspace>\n" +
		"# 目录架构与读写权限（下列均为完整绝对路径，直接使用，不要自行拼接）：\n" +
		"# " + p("workspace") + "          工作目录，草稿/脚本/命令产物放这里，自由读写（terminal 默认执行目录：" + filepath.ToSlash(workDir) + "）\n" +
		"# " + filepath.ToSlash(filepath.Join(workDir, "tmp")) + "   用户上传附件的暂存目录；需要附件内容时用 read_file 按路径读取（图片会作为图片消息进入你的上下文，无需调用识别工具）\n" +
		"# " + p("memory/longterm") + "    长期记忆，可写：harness.md 是索引（已注入上下文），主题文件按需新建，沉淀用户偏好与重要事实\n" +
		"# " + p("memory/skills") + "      技能库，可写：每技能一个子目录（SKILL.md 指令 + scripts/ 脚本），新建后下个 session 进清单\n" +
		"# " + p("apps") + "               快应用目录，由 save_app 工具写入，一般不手动改\n" +
		"# " + p("mcp.json") + "           MCP 服务配置，可写：新增/修改 server 后经 mcp_router 调用（资源变更会出现在轮首 <res_change> 提示）\n" +
		"# " + p("sessions") + "           历史会话存档，只读：上下文与回忆来源（compact 摘要引用其路径），改写会破坏会话链\n" +
		"# " + p(".ezloop/offload") + "    大工具结果的卸载区，按需读取，不手动管理\n" +
		"# " + p("settings.json") + " / " + p("models.json") + " / " + p("stats.json") + " / " + p("topics.json") + "：应用配置与索引，由设置页和应用自身管理，不要直接改写\n" +
		"# 规则：terminal 每条命令是独立进程（cd 不跨命令保留）；所有文件读写与命令一律绝对路径，不要依赖当前目录；\n" +
		"# 工作目录之外的临时文件不要随手乱放。\n" +
		"# 参数语法糖：工具参数的字符串值里写 <@toolArg>绝对路径</@toolArg>，执行时会自动展开为该文件内容" +
		"（省去先 read_file 再复制的往返；单文件上限 200000 字符，读不到会报错）；\n" +
		"# 输出占位：回复中给用户可点击的入口用 <$supper_url>类型://标识</$supper_url> 包裹——" +
		"https:// 外部链接、term://终端id（term_list 可查；长驻程序运行中或任务收尾时把终端入口交付给用户）、" +
		"app://快应用名（save_app 生成后在回复中引用，用户点击即开）、" +
		"file://工作目录内文本文件的绝对路径（write_file/read_file 等操作过的代码与文档，交付入口供用户点击查看编辑）；\n" +
		"# 共享终端（term_start/term_send 等）：魔法看板里的多终端，用户与你实时共见同一屏幕，全局共享（所有会话可用同一批终端）；" +
		"term_list 查看全部（含用户手开的），term_start 新建（带描述，可附带首条命令）；\n" +
		"# term_send 发命令并等输出静默返回（也用于应答交互/发 \\u0003 中断），term_read 游标式续读（只返回新增），term_close 关闭；\n" +
		"# 需要交互式应答/状态保留/长驻程序/想让用户看到过程时用 term_* 系列，一次性无状态命令仍用 terminal；\n" +
		"# 用户手动在终端里的操作会出现在轮首 <res_change> 资源变更提示里，留意并在需要时接续。\n" +
		"</workspace>")
	memRoot := filepath.ToSlash(filepath.Join(dataDir, "memory"))
	b.WriteString("\n\n<memory>\n" +
		"# 长期记忆（下列均为完整绝对路径，直接使用，不要自行拼接）\n" +
		"- 索引 " + memRoot + "/longterm/harness.md：长期记忆入口，全文见下方，可用文件工具直接更新\n" +
		"- 主题记忆 " + memRoot + "/longterm/：按主题的记忆文件（如 user.md），按需创建，不进上下文，用 findstr/grep 检索\n" +
		"- 技能 " + memRoot + "/skills/：沉淀的技能，每技能一个子目录（清单见 <skills>）\n" +
		"- 话题存档 " + filepath.ToSlash(filepath.Join(dataDir, "sessions")) + "/：历史会话全文（compact 后的旧库；在数据目录下，不在 memory 里）\n" +
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
		b.WriteString("\n\n<skills>\n（本清单由系统运行时生成，不在任何文件里；技能正文在 " +
			memRoot+"/skills/<名>/SKILL.md，可用文件工具编辑，改动下个 session 生效；"+
			"使用前先调用 load_skill 获取完整指令与脚本路径）")
		for _, sk := range skills {
			fmt.Fprintf(&b, "\n- %s: %s", sk.Name, sk.Description)
		}
		b.WriteString("\n</skills>")
	}
	if lines := mcpListLines(fsys); len(lines) > 0 {
		b.WriteString("\n\n<mcp>\n（经 mcp_router 工具调用，先用 mcp_list/tool_list 发现服务与工具）")
		for _, l := range lines {
			b.WriteString("\n" + l)
		}
		b.WriteString("\n</mcp>")
	}
	return b.String()
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
			out = append(out, "- "+srv.Name+": "+srv.Description)
		}
	}
	return out
}

/* termReportFn 共享终端状态面（remind 变更段对比基线用：终端清单变更 +
用户手动输入收割）；nil 服务返回 nil。终端全局共享，清单实时全量——各会话的
ResSnapshot 基线独立对比（A 会话首轮见到 B 会话开的终端同样报"新增"，
模型各自知悉全局终端水位）。 */
func termReportFn(t *TerminalService) func() hooks.TermReport {
	if t == nil {
		return nil
	}
	return func() hooks.TermReport {
		var rep hooks.TermReport
		for _, info := range t.List() {
			rep.Terms = append(rep.Terms, hooks.StatusTerm{ID: info.ID, Name: info.Name, Exited: info.Exited, Origin: info.Origin})
		}
		for _, l := range t.CollectUserActivity() {
			rep.Lines = append(rep.Lines, hooks.UserAction{ID: l.ID, Line: l.Line})
		}
		return rep
	}
}

/* mcpStatusList 返回 MCP 清单（remind 变更基线用，描述前 8 字）。 */
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
		desc := []rune(srv.Description)
		if len(desc) > 8 {
			desc = desc[:8]
		}
		out = append(out, hooks.StatusMcp{Name: srv.Name, Desc: string(desc)})
	}
	return out
}
