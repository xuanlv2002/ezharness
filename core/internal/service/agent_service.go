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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

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

	"ezharness/core/internal/builtinskill"
	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
	"ezharness/core/internal/osfs"
	"ezharness/core/internal/tools"
	"ezharness/core/internal/warp/modeldump"
	"ezharness/core/internal/warp/noempty"
	"ezharness/core/internal/warp/reasoningeffort"
	"ezharness/core/internal/warp/toolarg"
	"ezharness/core/internal/warp/visionguard"
)

/* AgentService 装配领域会话的运行时。 */
type AgentService struct {
	Hub       *domain.Hub
	Term      *TerminalService // 共享终端（魔法看板），可空：term_* 工具与状态注入的前提
	Browser   *BrowserService  // 共享浏览器桥（魔法看板），可空：browser_* 工具注入的前提
	McpRouter *mcp.Router      // 系统级 MCP router（全局单例注入），可空：不装配 mcp hook
	McpAuth   *McpAuthManager  // MCP OAuth 管理器（组 OAuth 配置用），可空
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

/* allModelEntries 汇总三槽全部条目（model_call/task 按名取模型的取值域）。 */
func allModelEntries(m domain.ModelsConfig) []domain.ModelEntry {
	out := make([]domain.ModelEntry, 0, len(m.Main)+len(m.Image)+len(m.Audio))
	out = append(out, m.Main...)
	out = append(out, m.Image...)
	out = append(out, m.Audio...)
	return out
}

/* findModelEntry 按名在三槽里找条目（已关闭的视同不存在——task 分身只能选可用清单内的模型）。 */
func findModelEntry(m domain.ModelsConfig, name string) *domain.ModelEntry {
	all := allModelEntries(m)
	for i := range all {
		if all[i].Name == name && !all[i].Disabled {
			return &all[i]
		}
	}
	return nil
}

/*
	modelFactoryOf 产出 task 分身的模型工厂：按名取条目（三槽任意）现造 provider，

未知名字返回 nil（task 把"unknown model"作为工具结果回传，模型自纠）。
包级函数而非内联闭包：Assemble 里 provider 是局部变量，会遮蔽 provider 包名。
*/
func modelFactoryOf(hub *domain.Hub) func(string) provider.ModelProvider {
	return func(name string) provider.ModelProvider {
		if e := findModelEntry(hub.ModelsSnapshot(), name); e != nil {
			return buildProvider(e)
		}
		return nil
	}
}

/* OneShotInvoke 用指定模型条目做一次无工具请求并返回文本（model_call 工具与 HTTP /api/open/model/call 共用）。 */
func OneShotInvoke(ctx context.Context, entry domain.ModelEntry, req types.ModelRequest) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	resp, err := buildProvider(&entry).Invoke(ctx, &req)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

/* absSkillsDir 技能库的绝对路径（提示里给模型的路径一律绝对）。 */
func absSkillsDir() string {
	abs, err := filepath.Abs(hooks.SkillsDir)
	if err != nil {
		return hooks.SkillsDir
	}
	return filepath.ToSlash(abs)
}

/*
	renderMemoryItems 渲染条目集合为 "- [id] text（更新于 YYYY-MM-DD）" 行

（按更新时间新→旧排序；空集合给占位符）。
*/
func renderMemoryItems(items []hooks.MemoryItem) string {
	if len(items) == 0 {
		return "（暂无）"
	}
	sorted := make([]hooks.MemoryItem, len(items))
	copy(sorted, items)
	slices.SortStableFunc(sorted, func(a, b hooks.MemoryItem) int {
		return b.UpdatedAt.Compare(a.UpdatedAt) // 新的在前
	})
	var b strings.Builder
	for _, it := range sorted {
		b.WriteString("- [" + it.ID + "] " + strings.ReplaceAll(it.Text, "\n", " ") + memoryUpdatedHint(it.UpdatedAt) + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

/*
	memoryUpdatedHint 渲染条目的更新日期（零值给空）。给绝对日期而非"距今

X"：system 段每 session 组装一次并固化，相对时长会随对话进行越变越不准，
绝对日期永不漂移，由模型自己与每轮 <agent_status> 的当前时间比对。
*/
func memoryUpdatedHint(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return "（更新于 " + t.Format("2006-01-02") + "）"
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
		sys = hooks.NewSysPrompt(buildSystemBase(ctx, st, s.Fsys, a.Hub.ModelsSnapshot()), "")
	}
	s.SetSysP(sys)
	sys.SetIdentityFn(func() string { return hooks.SessionIdentityBlock(s.ID) }) // 会话身份：ID+存档路径（trim 折叠后的回忆入口）
	s.Sess.BindSys(sys, main.Name)

	// 审批三档（全局，实时读取即改即生效）：off 全放行 / strict 全审 / '' 跟随安全配置
	approver, approveCh := approve.New(func(c *types.ToolCall) bool {
		switch a.Hub.SettingsSnapshot().ApproveMode {
		case "off":
			return false
		case "strict":
			return true
		}
		return a.needsApprove(c)
	})
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
	// 主模型摘要（快照行 + 变更基线共用）：换模型 Reassemble 后读新快照，
	// 轮首 diff 出「切换模型到 …」与 skill/mcp 同通道进上下文
	remindHook.SetModelBrief(func() string {
		m := a.Hub.ModelsSnapshot().ActiveMain()
		if m == nil || m.Name == "" {
			return ""
		}
		if m.Vision {
			return m.Name + "（支持视觉）"
		}
		return m.Name + "（不支持视觉）"
	})
	// 可选模型清单（与 skill/mcp 同构）：新增/移除随资源变更进上下文；
	// 清单本体在 system 的 <models> 固定段（session 组装时存储）
	remindHook.SetModelList(func() []hooks.StatusMcp { return modelStatusList(a.Hub.ModelsSnapshot()) })
	// 启停基线：停用名单与启用清单互补，区分「新增/移除」与「启用/停用」
	remindHook.SetMcpOff(func() []string { return mcpOffList(s.Fsys) })
	remindHook.SetModelOff(func() []string { return modelOffNames(a.Hub.ModelsSnapshot()) })
	// 计划按分支根持久化（compact 换代不丢），每轮注入 agent_status
	remindHook.SetPlanRoot(s.RootID)
	traceHook := hooks.NewTrace(s.Fsys, s.Sess, func() string { return main.Name })
	trimHook := hooks.NewTrim(provider, traceHook,
		window*st.TrimPercent/100,             // 水位=窗口百分比，随模型自适应（换模型 Reassemble 重算）
		window,                                // 模型窗口（整理提示展示水位比例用）
		s.Fsys, func() string { return s.ID }, // 进度档案 progress.md 落盘
	)

	// 主模型视觉能力实时判断（换模型 Reassemble 后随设置即时生效）
	mainVision := func() bool {
		m := a.Hub.ModelsSnapshot().ActiveMain()
		return m != nil && m.Vision
	}
	// read_file 图片分支裁决：开视觉 → 走标记→OnLoop 转图片消息（持久化）；
	// 未开 → 模型无法处理图片
	readImage := func(path, _ string) (string, bool) {
		if mainVision() {
			return "", true
		}
		return fmt.Sprintf("[当前模型无多模态能力，无法读取图片 %s；如需识别图片请在设置·模型为主模型勾选\"支持视觉\"]", path), false
	}

	// visionguard 最内层（紧贴 provider）：无视觉模型每次实际请求（含 retry）
	// 剥历史图片消息（请求视图，落盘不动，换回多模态自动恢复）
	// 思考档位：请求侧覆盖（闭包实时读设置，输入框切换即改即生效）
	effortWarp := reasoningeffort.Warp(func() string { return a.Hub.SettingsSnapshot().ReasoningEffort })
	// 重试：退避 0.5s 起指数翻倍（0.5/1/2/4/8/16/32/64s，共 8 次退避、
	// 总尝试 9 次）——并发分身挤出的 429 要等得起；不设总时间上限
	retryWarp := modelretry.Warp(func(o *modelretry.Options) {
		o.MaxAttempts = 9
		o.BaseDelay = 500 * time.Millisecond
	})
	modelWarps := []warp.ModelHandler{modeldump.Warp(), effortWarp, retryWarp, noempty.Warp(), visionguard.Warp(mainVision)}
	agentTools := append(append(tools.SaveApp(s.Fsys), tools.Sleep()), tools.SharedTerm(a.Term)...)
	agentTools = append(agentTools, tools.SharedBrowser(a.Browser)...)
	// model_call：把一次性子任务（识别/翻译/改写）交给指定模型；视觉任务走这里
	agentTools = append(agentTools, tools.ModelCall(s.Fsys,
		func() []domain.ModelEntry { return allModelEntries(a.Hub.ModelsSnapshot()) },
		func() *domain.ModelEntry { return a.Hub.ModelsSnapshot().ActiveMain() },
		OneShotInvoke))
	// memory：长期记忆唯一写入通道（提案制，用户在记忆页审阅）
	agentTools = append(agentTools, tools.Memory(s.Fsys))
	// 快应用：清单随 <apps> 段、增删改进 <resource_change>（与技能同构）；工具只读调用文档
	agentTools = append(agentTools, tools.LoadApp(s.Fsys))
	// plan：会话级任务计划（落盘 + plan.update 事件驱动输入框上方进度面板）
	agentTools = append(agentTools, tools.Plan(s.Fsys, s.RootID, func(p *hooks.SessionPlan) {
		var data json.RawMessage
		if p != nil {
			if b, err := json.Marshal(p); err == nil {
				data = b
			}
		}
		if data == nil {
			data = json.RawMessage("null")
		}
		s.Publish(domain.Event{Type: "plan.update", Ts: time.Now().UnixMilli(), Data: data})
	}))
	if a.Browser != nil {
		a.Browser.SetVisionProbe(mainVision) // 截图返回形态随主模型视觉能力实时裁决
	}
	toolNames := []string{
		"read_file", "write_file", "edit_file", "terminal", "save_app",
		askuser.ToolName, task.ToolName,
		"mcp_router", hooks.TrimTool, skilltool.ToolName,
		"model_call", "memory", "plan", "load_app", "term_tab", "term_action",
		"browser_tab", "browser_action",
	}
	mcpHook, mcpAuthCh := NewMcpHook(context.Background(), s.Fsys, a.McpRouter, a.McpAuth)
	agent := core.NewAgent(provider,
		core.WithModelWarp(modelWarps...),
		core.WithToolWarp(toolarg.Warp(s.Fsys), limit.Warp(4), safetool.Warp()),
		core.WithTools(agentTools...),
		core.WithHooks(
			sys,                          // startHooks 首位：system base 唯一来源；后续 hook 在其 OnStart 里追加 tool-guide 说明段
			traceHook,                    // toolStart 首位：task/ask_user/load_skill 等 OnToolStart 内干活的 hook 返回 Skip 会短路后续 hook，观测层必须排在它们前面才有 span
			hooks.NewLoopGuard(maxIters), // 循环护栏：trace 后、Skip 型 hook 前——同签名重复先提醒后拦截,轮数压力线强收敛
			contextfix.New(),
			filetools.New(s.Fsys, filetools.WithWorkDir(ResolveWorkDir(st.WorkDir)), filetools.WithImageHandler(readImage)),
			skilltool.New(s.Fsys, absSkillsDir(), disabledSkills, // 绝对目录：load_skill 返回的技能路径被模型直接拿去读写
				skilltool.WithExtra(builtinskill.Skills)), // 内建技能不落磁盘，直接进 load_skill 名称表
			remindHook,         // 系统提醒：变更段插 <resource_change>? + 快照段插 agent_status；OnEnd 收尾 <end_reason>
			hooks.NewRefFile(), // 有引用轮次在输入前插 <reference_file> 结构化告知（附件+文件页标注统一，模型按需 read_file）
			approver,
			asker,
			task.New(task.WithModelFactory(modelFactoryOf(a.Hub)),
				// 子会话存档地址随结果回传，主模型可按需 read_file 回查过程
				task.WithSessionRef(func(forkID string) string {
					p, err := filepath.Abs(filepath.Join(hooks.SessionsDir, s.Sess.ID(), "forks", forkID))
					if err != nil {
						return ""
					}
					return filepath.ToSlash(p)
				})),
			mcpHook,
			offload.New(s.Fsys, offload.WithThreshold(st.OffloadThreshold),
				/* 摘要头随阈值分档但封顶（默认 512，最大 1KB），避免
				   阈值调大后卸载结果仍占满上下文。 */
				func(o *offload.Options) { o.Head = offloadHead(st.OffloadThreshold) },
				offload.WithSkip(askuser.ToolName, task.ToolName, skilltool.ToolName), offload.WithReplayTool("read_file"),
				offload.WithAbs(func(p string) string { // 提示给绝对路径：模型不知道 FS 挂载基准，按工作目录拼相对路径必错
					if abs, err := filepath.Abs(p); err == nil {
						return filepath.ToSlash(abs)
					}
					return p
				})), // load_skill 返回的指令集是后续行动依据,卸载再回读纯浪费
			hooks.NewGuard(s.Fsys, window), // 窗口余量兜底：offload 豁免名单（read_file 等）的大结果放不下时卸载，须在 offload 之后
			trimHook,                       // OnLoop 回边水位整理（就地截断，立即生效），OnToolStart 拦模型主动整理
			s.Sess,                         // 最后落盘
		),
		core.WithLoopParams(core.LoopParams{MaxIterations: maxIters}),
		core.WithStreaming(true),
	)

	s.Attach(domain.Wiring{
		Agent:     agent,
		Provider:  provider,
		ApproveCh: approveCh,
		AnswerCh:  answerCh,
		McpAuthCh: mcpAuthCh,
		Trace:     traceHook,
		ToolNames: toolNames,
	})
}

/*
	Reassemble 重建全部存活分支的 agent（配置变更后；运行中的分支

跳过——保留旧 wiring 到其轮结束，下次变更追平）。活动分支 busy
仍返回 ErrBusy 保持前端提示语义。
*/
func (a *AgentService) Reassemble(st domain.Settings) error {
	if a.Hub.ActiveSession().Busy() {
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

// offloadHead 返回卸载后保留在上下文里的摘要头部字节数：随阈值分档
// 但封顶。默认阈值 4096 → 512 字节；阈值 >= 8192 时最多 1024 字节；
// 极小阈值下不超过阈值一半。卸载的目的是缩小上下文，摘要头若与阈值
// 等长，多个大结果很快又把上下文灌满，模型仍要 read_file 补细节，
// 等于两头都输。
func offloadHead(threshold int) int {
	if threshold <= 0 {
		threshold = offload.DefaultThreshold
	}
	head := threshold / 8
	if head < 256 {
		head = 256
	}
	if head > 1024 {
		head = 1024
	}
	if head > threshold/2 {
		head = threshold / 2
	}
	return head
}

/*
	needsApprove 按审批策略判定。

查表键是工具名；合并式工具（term_action / browser_action）先试
"工具名:action"。只读清单类动作恒免审；未知工具默认审批。运行时读设置
快照（缺项回落内置默认），改策略即时生效（无需重建 agent）。
*/
func (a *AgentService) needsApprove(c *types.ToolCall) bool {
	name, action := ruleKeyOf(c)
	if c.Name == "mcp_router" && (action == "mcp_list" || action == "tool_list") {
		return false // 发现类只读无副作用，恒免审
	}
	if (name == "browser_tab" || name == "term_tab") && action == "list" {
		return false // 标签清单只读无副作用，恒免审（new/open/close 才是管控面）
	}
	rule := findRule(a.Hub.ToolRulesSnapshot(), name, action)
	if rule == nil {
		rule = findRule(domain.DefaultToolRules(), name, action) // 存量档缺项回落默认
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
		// 名单档仅有名单能力的 key 支持（命令 / 路径 / mcp）；其余按需审批兜底
		if domain.ListKindOf(rule.Key) == "" {
			return true
		}
		hit := matchRuleList(rule.List, rule.Level, rule.Key, c.Args)
		if rule.Level == domain.LevelWhite {
			return !hit
		}
		return hit
	}
	return true
}

/*
ruleKeyOf 归一调用的查表基准：mcp_router 二段式映射为 mcp.* 伪工具；
其余返回 (工具名, action)（无 action 字段的工具 action 为空）。
*/
func ruleKeyOf(c *types.ToolCall) (string, string) {
	action := callAction(c.Args)
	if c.Name == "mcp_router" {
		return "mcp.*", action
	}
	return c.Name, action
}

/* callAction 从参数取 action（无该字段的工具返回空串）。 */
func callAction(args json.RawMessage) string {
	var a struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(args, &a)
	return a.Action
}

/* findRule 先按 "工具名:action" 精确查，再按工具名查。 */
func findRule(rules []domain.ToolRule, name, action string) *domain.ToolRule {
	if action != "" {
		key := name + ":" + action
		for i := range rules {
			if rules[i].Key == key {
				return &rules[i]
			}
		}
	}
	for i := range rules {
		if rules[i].Key == name {
			return &rules[i]
		}
	}
	return nil
}

/*
matchRuleList 判定调用是否命中名单，语义随名单类型与档位：
command 白名单=前缀（命令以名单词开头，词边界=结尾或空白）、黑名单=包含；
path 白名单=路径前缀（分隔符归一，Windows 忽略大小写）；无黑名单；
mcp 白名单=server 或 server.tool 精确/整站点边界、黑名单=子串包含。
*/
func matchRuleList(list []string, level domain.Level, ruleKey string, args json.RawMessage) bool {
	white := level == domain.LevelWhite
	switch domain.ListKindOf(ruleKey) {
	case "command":
		var a struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(args, &a)
		return matchCommand(list, white, strings.TrimSpace(a.Command))
	case "path":
		var a struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(args, &a)
		return matchPathPrefix(list, a.Path)
	case "mcpTool":
		var a struct {
			Server string `json:"server"`
			Tool   string `json:"tool"`
		}
		_ = json.Unmarshal(args, &a)
		key := a.Server
		if a.Tool != "" {
			key = a.Server + "." + a.Tool
		}
		return matchMcp(list, white, key)
	}
	return false
}

/*
shellMeta 是白名单免审的否决字符：拼接（; && || &）、管道、子命令
（$() 与反引号）、重定向（< >）和换行都可能在名单词之后改变语义，
出现即转审批（宁误拦不漏拦）。
*/
const shellMeta = ";&|`$<>\n\r"

/* matchCommand 终端命令词：白名单前缀命中（词边界），黑名单包含命中。 */
func matchCommand(list []string, white bool, cmd string) bool {
	if cmd == "" {
		return false
	}
	if white && strings.ContainsAny(cmd, shellMeta) {
		return false
	}
	for _, e := range list {
		if white {
			if cmd == e || (strings.HasPrefix(cmd, e) && isSpaceByte(cmd[len(e)])) {
				return true
			}
		} else if strings.Contains(cmd, e) {
			return true
		}
	}
	return false
}

/* matchPathPrefix 路径白名单：前缀命中即免审（分隔符归一，Windows 忽略大小写）。 */
func matchPathPrefix(list []string, path string) bool {
	if path == "" {
		return false
	}
	p := filepath.ToSlash(filepath.Clean(path))
	for _, e := range list {
		pre := filepath.ToSlash(filepath.Clean(e))
		if runtime.GOOS == "windows" {
			if strings.HasPrefix(strings.ToLower(p), strings.ToLower(pre)) {
				return true
			}
		} else if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

/* matchMcp server.tool：白名单精确/整站点前缀（点边界），黑名单子串包含。 */
func matchMcp(list []string, white bool, key string) bool {
	if key == "" {
		return false
	}
	for _, e := range list {
		if white {
			if key == e || strings.HasPrefix(key, e+".") {
				return true
			}
		} else if strings.Contains(key, e) {
			return true
		}
	}
	return false
}

/* isSpaceByte 命令词边界字符（空白）。 */
func isSpaceByte(b byte) bool { return b == ' ' || b == '\t' }

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
func buildSystemBase(ctx context.Context, st domain.Settings, fsys osfs.OS, mc domain.ModelsConfig) string {
	var b strings.Builder
	b.WriteString("你是 ezharness——一个持续陪伴用户的设备级 agent，可全权操作本机文件与命令。" +
		"能用工具就用工具，回答简洁。" +
		"用户需要小工具或网页时用 save_app 生成为快应用，用户可一键启动（需要后端进程的见 quick-app 技能）；已有快应用先看 <apps>，使用前 load_app 读调用文档。" +
		"重要的用户偏好与事实用 memory 工具随手读写长期记忆（结构见 <memory> 块）。" +
		"接任务先看 <action> 行动准则；给用户的可点击入口与输出格式遵守 <output>。")
	if st.SystemExtra != "" {
		b.WriteString("\n\n" + st.SystemExtra)
	}
	dataDir, _ := os.Getwd() // 进程 cwd 即数据目录（启动时 chdir）
	workDir := ResolveWorkDir(st.WorkDir)
	p := func(rel string) string { return filepath.ToSlash(filepath.Join(dataDir, rel)) }
	memRoot := p("memory")
	b.WriteString("\n\n<workspace>\n" +
		"# 数据目录：" + filepath.ToSlash(dataDir) + "（配置 mcp.json、记忆 memory/、会话 sessions/ 的根；本块与 <memory> 段给出的路径均为绝对路径，直接使用，不要自行拼接或再拼相对前缀）\n" +
		"# 工作区：三个可写位置\n" +
		"# " + p("workspace") + "   工作目录，草稿/脚本/命令产物一律放这里（terminal 默认执行目录：" + filepath.ToSlash(workDir) + "）；" +
		"其下 tmp/ 是用户上传附件的暂存处，需要附件内容时用 read_file 按路径读取（图片会作为图片消息进入你的上下文，无需调用识别工具）\n" +
		"# 长期记忆——不提供路径（读写纪律见 <memory> 段：写入只走 memory 工具提案，项目记忆按索引行地址读取）\n" +
		"# " + memRoot + "/skills     技能库：每技能一个子目录（SKILL.md 指令 + scripts/ 脚本），新建后下个 session 进清单\n" +
		"# " + p("apps") + "   快应用目录：每个应用一个子目录（声明 app.quick + 前端 + 可选后端脚本），构建约定见 quick-app 技能\n" +
		"# 行为规则（不需要记路径，按规则做即可）：\n" +
		"# - 技能清单以 <skills> 段、MCP 服务以 <mcp> 段为准，不要读目录或配置文件去发现它们；\n" +
		"# - 快应用是 apps/<名>/ 目录应用：前端用 save_app 写，后端脚本等额外文件用 write_file 补进同目录（约定见 quick-app 技能），不要手改其他快应用的文件；\n" +
		"# - 本会话的存档与进度档案路径见 <session> 块（回忆入口）；其他历史会话的存档不要主动翻阅，确有需要先问用户；\n" +
		"# - 超长工具结果会被系统自动卸载为文件并在工具结果里给出路径，按提示 read_file 取回，不要主动浏览卸载区；\n" +
		"# - 数据目录下的配置与索引文件（settings、models、stats、topics 等）由应用管理，不要改写；\n" +
		"# - 路径纪律：用户给了路径就把检索与读写限定在该路径内，没给路径就只在上面的工作目录内；" +
		"禁止对 C:/ 根、用户主目录、桌面等宽泛位置做递归搜索或遍历（find/grep 全盘极易超时卡死且对结论无贡献）；" +
		"缺少路径信息先问用户，不要自行上溯目录猜路径；\n" +
		"# - terminal 每条命令是独立进程（cd 不跨命令保留）；所有文件读写与命令一律绝对路径，临时文件不要丢在工作目录外。\n" +
		"# - 定时等待用 sleep 工具（真阻塞，用户停止即中断）；terminal/term_action send 里跑 timeout 之类的睡眠命令在 Windows 下立即退出，等不住。\n" +
		"</workspace>")
	userItems, _ := hooks.LoadItems(ctx, fsys, hooks.UserJson)
	soulItems, _ := hooks.LoadItems(ctx, fsys, hooks.SoulJson)
	projects, _ := hooks.LoadProjects(ctx, fsys)
	var projLines strings.Builder
	if len(projects) == 0 {
		projLines.WriteString("（暂无）")
	} else {
		for _, pr := range projects {
			projLines.WriteString("- " + pr.ID + " — " + pr.Name + " — " + pr.Description + "\n")
		}
	}
	b.WriteString("\n\n<memory>\n" +
		"# 长期记忆（条目式，按更新时间新→旧排；[id] 标注；条目后的「更新于 YYYY-MM-DD」是最近更新时间——" +
		"与当前时间（每轮见 <agent_status> 的「当前时间」）比对，间隔过久的当作可能已漂移的旧信息；" +
		"项目详情按 id 用 memory 工具 project-read 读取）\n" +
		"# user（用户个人信息）\n" + renderMemoryItems(userItems) +
		"\n# soul（你的灵魂：用户对你的期望——性格、表达方式、工作方式与协作原则；项目相关的硬性约束写项目记忆）\n" + renderMemoryItems(soulItems) +
		"\n# project（项目索引：id — 名称 — 描述）\n" + strings.TrimSuffix(projLines.String(), "\n") +
		"\n# 读写纪律（现行，优先级最高）\n" +
		"# - 记忆只能用 memory 工具读写：条目 add/update/remove（target=user|soul，id 用上面的标注），项目 project-save/project-remove/project-read；不可用文件工具读写 memory/ 下任何文件\n" +
		"# - 提交的变更先存待确认区，用户在通知栏确认后才生效（取消即丢弃）：同一条信息不要重复提交，生效与否以下一轮 resource_change 为准\n" +
		"# - 发现值得长期保留的用户信息、或用户对你（soul）的要求随手提交；过时条目随手提交 remove，宁精勿杂\n" +
		"# - 归档时系统也会自动生成记忆修改方案（同样待用户确认）\n" +
		"\n</memory>")
	skills := builtinskill.Skills()
	if user, err := skill.LoadDir(ctx, fsys, hooks.SkillsDir); err == nil {
		// 与内建同名的用户技能让位（内建优先），清单与记忆页同一归置
		for _, sk := range user {
			if !slices.ContainsFunc(skills, func(b skill.Skill) bool { return hooks.SkillDirOf(b.Path) == hooks.SkillDirOf(sk.Path) }) {
				skills = append(skills, sk)
			}
		}
	}
	if len(skills) > 0 {
		b.WriteString("\n\n<skills>\n（可用技能清单，以此为准，不要读取 memory/skills 目录来发现技能；" +
			"技能正文在 " + memRoot + "/skills/<名>/SKILL.md，可用文件工具编辑，改动下个 session 生效，" +
			"轮内变更（新增/移除/启用/停用）见 <resource_change>；使用前先调用 load_skill 获取完整指令与脚本路径。" +
			"标注〔内建〕的技能随应用分发，正文同样可 load_skill 查看）")
		var offNames []string
		for _, sk := range skills {
			if slices.Contains(st.DisabledSkills, hooks.SkillDirOf(sk.Path)) {
				offNames = append(offNames, sk.Name)
				continue
			}
			desc := sk.Description
			if desc == "" {
				desc = "（无描述）"
			}
			mark := ""
			if builtinskill.IsBuiltin(hooks.SkillDirOf(sk.Path)) {
				mark = "〔内建〕"
			}
			fmt.Fprintf(&b, "\n- %s%s: %s", sk.Name, mark, desc)
		}
		// 停用的只报名字（省上下文）：存在但 load_skill 会拒绝，模型不去调用也不会翻目录找
		if len(offNames) > 0 {
			slices.Sort(offNames)
			b.WriteString("\n已停用（存在但不可用，load_skill 会拒绝，不要尝试；用户需要时会在记忆页启用）：" +
				strings.Join(offNames, "、"))
		}
		b.WriteString("\n</skills>")
	}
	b.WriteString(renderAppsSection(ctx, fsys))
	// 可用模型清单（固定段，session 组装时存储；与 skill/mcp 同构）：task/model_call
	// 的 model 参数取值以此为准；增删随 <resource_change> 告知，主模型每轮见 <agent_status>
	if ms := modelStatusList(mc); len(ms) > 0 {
		b.WriteString("\n\n<models>\n（可用模型清单：task 与 model_call 的 model 参数取值以此为准，不要臆造；" +
			"当前主模型每轮见 <agent_status>，增删与主模型切换见 <resource_change>）")
		for _, m := range ms {
			b.WriteString("\n- " + m.Name + "（" + m.Desc + "）")
		}
		b.WriteString("\n</models>")
	}
	if lines := mcpListLines(fsys); len(lines) > 0 {
		b.WriteString("\n\n<mcp>\n（可用 MCP 服务清单，以此为准，不要读取 mcp.json 来发现服务；" +
			"经 mcp_router 工具调用，先用 tool_list 拉取某服务的工具清单再 tool_call；工具名与参数以 schema 为准，不要臆造；" +
			"页面/脚本直调见 <local_api>；轮内变更见 <resource_change>）")
		for _, l := range lines {
			b.WriteString("\n" + l)
		}
		b.WriteString("\n</mcp>")
	}
	// core OpenAPI 索引：载荷、示例、SDK 与授权处理的完整用法在 open-api 技能
	b.WriteString("\n\n<local_api>\n" +
		"（core 本机 OpenAPI：POST /api/open/model/call 直调大模型、POST /api/open/mcp/call 调 MCP 工具、GET /api/open/mcp 服务清单。\n" +
		"鉴权：请求头 X-EZ-Open-Token: $EZHARNESS_OPENAPI_TOKEN（Windows %EZHARNESS_OPENAPI_TOKEN%，终端与子进程自动继承）；" +
		"外部程序读 <数据目录>/openapi.json。浏览器页面引 /api/open/sdk.js 后 ez.config 无脑传令牌即可。\n" +
		"写调用代码前先 load_skill 加载 open-api 技能拿载荷与示例；401 多半是令牌被重置，重取令牌即可，不要让用户把令牌贴进对话。）")
	if base := localAPIBase(); base != "" {
		b.WriteString("\n完整地址：" + base + "/api/open/<路径>。")
	}
	b.WriteString("\n</local_api>")
	b.WriteString("\n\n<action>\n" +
		"# 行动准则\n" +
		"1. 接任务先匹配技能：对照 <skills> 清单，命中就 load_skill 加载后按其指引执行，不要绕过现成技能自己造流程；没命中再自行设计。\n" +
		"2. 动手前想清楚：多步骤或有风险的任务，先用 ask_user 提交计划（options 给候选，如 [\"执行\",\"否决\",\"修改\"]）请用户处置，获批后再执行；一步能完成的小事直接做。\n" +
		"3. 执行工具的选择：\n" +
		"- 本机一次性命令（构建、查询、跑脚本）用 terminal——它会阻塞到命令结束（或超时）；长任务（下载、构建、要 sleep 的活）就用它同步跑完。\n" +
		"- 需要交互式应答、长驻程序、或希望用户实时看到过程时，用共享终端：term_tab 管标签（new 新建 / list 查全部含用户手开的 / close 关闭），term_action action=send 发命令（也用于应答交互或发中断信号 \\u0003）——它等输出静默后直接把这次命令的输出返回，短命令一条调用就拿到结果，长驻命令到点先返回、后续用 action=read 读末尾 N 行（默认 100，读多少次都是同一份）；长驻程序运行中或任务收尾时把终端入口交付给用户；\n" +
		"- 检索网页、查资料、操作网页用共享浏览器（真实 Chromium，用户实时共见可直接接管；browser_tab 管标签，open 自动展开浏览器页、首次使用会自动下载 Chromium 需等待；browser_action 只有七个动作——navigate 打开网址、read 读正文/DOM/可交互元素/链接/子帧、screenshot 截图（默认视口，可整页或只截某元素）、eval 页面内执行 JS（**页面里的点击/填值/滚动/断言都走它**：__ezOne(选择器) 深查询穿透 shadow DOM 与同源 iframe，没有 click/type 这类单独动作，自己写 JS 做，别靠截图估坐标；脚本自动按 async 包裹——顶层可直接 await，末尾表达式自动作为返回值（同浏览器 console），也可显式 return，真实异常会原样回传）、console 读控制台、network 读网络请求、upload 传文件；读不到内容时先 eval 里 await 一下或轮询，再 read。原生 alert/confirm 一律自动取消并记进控制台；与 terminal（本机命令）互补。\n" +
		"4. MCP：内置工具够用就不绕道；用 MCP 时先 mcp_list / tool_list 发现能力再 tool_call，不凭记忆猜工具名和参数。\n" +
		"5. 可并行、相互独立、或会产生大量中间输出的子任务，交 task 分身执行（工具集相同、过程互不干扰，结果直接回传），主对话只接结论；任务描述必须自包含（分身看不到本轮对话之外的语境）：写清目标、输入、涉及的文件绝对路径与期望的返回格式；无依赖的子任务一次并行发多个。\n" +
		"6. 交付与连续性：任务收尾把成果入口用 <$supper_url> 交付（格式见 <output>），产物文件放工作目录；" +
		"上下文被整理后，从 <session> 块告知的进度档案恢复现场接着干，长任务到达阶段性节点时也可主动把进度补写进该档案。\n" +
		"7. 重试纪律：同一手段连续 2 次无可见效果立即停——没有新信息不再重复，换手段（如滚不动换 key 翻页）或先建可观测信号（读返回的 url/title/scrollY、截图）确认状态再动；" +
		"调试先求\"能看见状态\"，再动目标；同一子任务调用约 10 次仍无进展即停手向用户汇报现状与已试手段；长任务每几步用一句话报进度，不让用户猜你在干嘛。\n" +
		"</action>")
	b.WriteString("\n\n<output>\n" +
		"# 输出规范\n" +
		"## 可点击入口 <$supper_url>（格式错就不会渲染成可点击入口，用户只能看到原文，务必照抄格式）\n" +
		"规则：整段闭合包裹、标签紧贴地址、地址内不能有空格/换行/文字；说明文字写在标签外面。\n" +
		"正确示例：\n" +
		"<$supper_url>https://example.com/report</$supper_url>\n" +
		"<$supper_url>term://终端id</$supper_url>（id 来自 term_tab）\n" +
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

/* localAPIBase 返回本机 API 基址（供 <local_api> 段告知完整地址；读不到配置返回空）。 */
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
	f := loadMcpFileOrNil(context.Background(), fsys)
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
	f := loadMcpFileOrNil(context.Background(), fsys)
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

/* mcpOffList 返回已停用 server 名（与 mcpStatusList 互补，启停基线 diff 用）。 */
func mcpOffList(fsys osfs.OS) []string {
	f := loadMcpFileOrNil(context.Background(), fsys)
	if f == nil {
		return nil
	}
	var out []string
	for _, srv := range f.Servers {
		if !srv.IsEnabled() {
			out = append(out, srv.Name)
		}
	}
	return out
}

/* modelOffNames 返回三槽中被关闭（disabled）的模型名（与 modelStatusList 互补）。 */
func modelOffNames(mc domain.ModelsConfig) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range allModelEntries(mc) {
		if e.Name == "" || !e.Disabled || seen[e.Name] {
			continue
		}
		seen[e.Name] = true
		out = append(out, e.Name)
	}
	return out
}

/* modelStatusList 汇总可选模型（三槽去重，名字+槽位/视觉能力），供 <models> 段与资源清单共用。 */
func modelStatusList(mc domain.ModelsConfig) []hooks.StatusMcp {
	seen := map[string]bool{}
	var out []hooks.StatusMcp
	for _, slot := range []struct {
		label   string
		entries []domain.ModelEntry
	}{{"对话", mc.Main}, {"图像", mc.Image}, {"语音", mc.Audio}} {
		for _, e := range slot.entries {
			if e.Name == "" || seen[e.Name] || e.Disabled {
				continue
			}
			seen[e.Name] = true
			v := "不支持视觉"
			if e.Vision {
				v = "支持视觉"
			}
			out = append(out, hooks.StatusMcp{Name: e.Name, Desc: slot.label + "槽 · " + v})
		}
	}
	return out
}
