/*
AgentService 负责 agent 的装配与重建：provider、hooks、warp、工具集。
文件与终端工具复用 ezloop 的 filetools hook（原生 shell，Windows 为 cmd，
模型适配环境），ezharness 只增补 save_app。

上下文机制：system 由 sysprompt hook 每轮注入（base 段文本唯一归属在
systemprompt.go，session 创建时组装一次；重启从快照还原不重组）。
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
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/ext/hook/contextfix"
	"github.com/xuanlv2002/ezloop/ext/hook/filetools"
	"github.com/xuanlv2002/ezloop/ext/hook/mcp"
	"github.com/xuanlv2002/ezloop/ext/hook/offload"
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
	Callbacks *CallbackService // 定时回调中心，可空：不装配 callback 工具
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
		sys = hooks.NewSysPrompt(buildSystemBase(ctx, st, s.Fsys, a.Hub.ModelsSnapshot(), s.CurrentID()), "")
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
	planner, planCh := hooks.NewPlanConfirm()

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
	// 定时回调：清单随 <callbacks> 段、增删改进 <resource_change>；工具是唯一配置入口
	if a.Callbacks != nil {
		agentTools = append(agentTools, tools.Callback(a.Callbacks, s.CurrentID))
	}
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
		"read_file", "write_file", "edit_file", "terminal", "save_app", "sleep",
		askuser.ToolName, task.ToolName,
		"mcp_router", hooks.TrimTool, skilltool.ToolName,
		"model_call", "memory", "plan", "load_app", "term_tab", "term_action",
		"browser_tab", "browser_action",
	}
	if a.Callbacks != nil {
		toolNames = append(toolNames, "callback")
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
			planner, // 计划确认排在审批之后：用户给 plan 开了审批就先弹审批，拒绝即短路，计划卡不出现
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
		PlanCh:    planCh,
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
