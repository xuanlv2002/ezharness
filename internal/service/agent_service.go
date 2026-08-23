/*
AgentService 负责 agent 的装配与重建：provider、hooks、warp、工具集。
文件与终端工具复用 ezloop 的 filetools hook（原生 shell，Windows 为 cmd，
模型适配环境），ezharness 只增补 save_app。

上下文机制：system 由 sysprompt hook 每轮注入（session 创建时组装一次：
人格+SystemExtra+长期记忆+skill/mcp 列表；重启从快照还原不重组）。
contextfix 修理残缺历史，offload 卸载大工具结果；压缩（compact）、
状态栏（status）、调用链（trace）见 internal/hooks 各文件。
*/
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/ext/hook/contextfix"
	"github.com/xuanlv2002/ezloop/ext/hook/filetools"
	"github.com/xuanlv2002/ezloop/ext/hook/offload"
	"github.com/xuanlv2002/ezloop/ext/hook/skill"
	"github.com/xuanlv2002/ezloop/ext/hook/task"
	"github.com/xuanlv2002/ezloop/ext/hook/taskplan"
	"github.com/xuanlv2002/ezloop/ext/provider/openai"
	"github.com/xuanlv2002/ezloop/ext/warp/model/modelretry"
	"github.com/xuanlv2002/ezloop/ext/warp/tool/limit"
	"github.com/xuanlv2002/ezloop/ext/warp/tool/safetool"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
	"ezharness/internal/osfs"
	"ezharness/internal/tools"
	"ezharness/internal/warp/modeldump"
)

/* AgentService 装配领域会话的运行时。 */
type AgentService struct {
	Hub *domain.Hub
}

/* Assemble 按配置装配 agent 并注入会话（主模型取 models 四槽 main 启用条目）。 */
func (a *AgentService) Assemble(s *domain.Session, st domain.Settings) {
	ctx := context.Background()

	main := a.Hub.ModelsSnapshot().ActiveMain()
	if main == nil {
		main = &domain.ModelEntry{}
	}
	provider := openai.New(openai.Options{
		BaseURL: main.BaseURL,
		APIKey:  main.APIKey,
		Headers: main.Headers,
		Model:   main.Name,
	})

	// system 两段式：恢复的会话从快照还原（记忆/skill/mcp 变更等下个
	// session），新会话组装一次后固定，直到 compact 创建新 session。
	var sys *hooks.SysPrompt
	if snap := s.Snapshot(); snap != nil {
		sys = hooks.NewSysPrompt(snap.SystemBase, snap.SummaryBlock)
	} else {
		sys = hooks.NewSysPrompt(buildSystemBase(ctx, st, s.Fsys), "")
	}
	s.SetSysP(sys)
	s.Sess.BindSys(sys, main.Name)

	approver, approveCh := approve.New(a.needsApprove)
	asker, answerCh := askuser.New()
	planner, planCh := taskplan.New()

	window := main.ContextWindow
	if window <= 0 {
		window = 128000 // 旧 models.json 无 contextWindow 字段的兜底
	}
	statusHook := hooks.NewStatus(s.Fsys, s.Sess,
		func() int { return a.Hub.Active.CtxTokens() },
		window,
		func() []hooks.StatusMcp { return mcpStatusList(s.Fsys) },
	)
	traceHook := hooks.NewTrace(s.Fsys, s.Sess, func() string { return main.Name })
	compactHook := hooks.NewCompact(provider, s.Fsys, s.Sess, sys, a.Hub.Topics, traceHook,
		a.Hub.SettingsSnapshot().CompactThreshold,
		func() string { return buildSystemBase(ctx, st, s.Fsys) }, // compact 即新 session：全量重载
		func(info hooks.CompactInfo) { a.Hub.Active.SetIdentity(info.NewID) },
	)

	agent := core.NewAgent(provider,
		core.WithModelWarp(modeldump.Warp(), modelretry.Warp()),
		core.WithToolWarp(limit.Warp(4), safetool.Warp()),
		core.WithTools(tools.SaveApp(s.Fsys)...),
		core.WithHooks(
			sys, // startHooks 首位：system 唯一来源
			contextfix.New(),
			filetools.New(s.Fsys),
			hooks.NewSkillTool(s.Fsys, hooks.SkillsDir),
			statusHook,
			approver,
			asker,
			planner,
			task.New(),
			NewMcpHook(s.Fsys),
			offload.New(s.Fsys, offload.WithSkip(askuser.ToolName, taskplan.ToolName, task.ToolName)),
			compactHook, // OnEnd 在 trace/store 之前：截断+换库先发生
			traceHook,
			s.Sess, // 最后落盘
		),
		core.WithLoopParams(core.LoopParams{MaxIterations: 12}),
		core.WithStreaming(true),
	)

	s.Attach(domain.Wiring{
		Agent:     agent,
		Provider:  provider,
		ApproveCh: approveCh,
		AnswerCh:  answerCh,
		PlanCh:    planCh,
		Trace:     traceHook,
		ToolNames: []string{
			"read_file", "write_file", "edit_file", "bash", "save_app",
			askuser.ToolName, taskplan.ToolName, task.ToolName,
			"mcp_router", hooks.CompactTool, hooks.SkillTool,
		},
	})
}

/* Reassemble 重建活动会话的 agent（配置变更后，需空闲）。 */
func (a *AgentService) Reassemble(st domain.Settings) error {
	s := a.Hub.Active
	if s.Busy() {
		return domain.ErrBusy
	}
	a.Assemble(s, st)
	return nil
}

/*
	needsApprove 按审批策略判定（安全页四档）。

人机交互与内部工具恒免审；未知工具默认审批。运行时读设置快照，
改策略即时生效（无需重建 agent）。
*/
func (a *AgentService) needsApprove(c *types.ToolCall) bool {
	switch c.Name {
	case askuser.ToolName, taskplan.ToolName, hooks.SkillTool, hooks.CompactTool:
		return false // 交互与内部工具不属用户管控面（加载技能/压缩均为只读元操作）
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
	rules := a.Hub.SettingsSnapshot().ToolRules
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
	case "terminal":
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
		if ruleTool == "terminal" && strings.HasPrefix(key, e+" ") {
			return true // 命令词边界
		}
		if ruleTool == "mcp.*" && strings.HasPrefix(key, e+".") {
			return true // server 前缀放行整站（点边界：time 不误命中 timeX）
		}
		if ruleTool != "terminal" && ruleTool != "mcp.*" && strings.HasPrefix(key, e) {
			return true // 路径前缀
		}
	}
	return false
}

/*
buildSystemBase 组装 session 的 system 基础段：人格 + SystemExtra +
标签化注入块（<memory> 长期记忆结构+索引 / <skills> 技能列表 /
<mcp> MCP 列表）。只在 session 创建时调用一次（同 session 不变）；
skill 全文与记忆细节不注入（模型按需用文件工具读取），列表变更要等
下个 session 才进 system，过渡期靠 agent_status 状态栏告知模型。
*/
func buildSystemBase(ctx context.Context, st domain.Settings, fsys osfs.OS) string {
	var b strings.Builder
	b.WriteString("你是 ezharness——一个持续陪伴用户的设备级 agent，可全权操作本机文件与命令。" +
		"能用工具就用工具，回答简洁。可并行的子任务用 task 分身去做。" +
		"用户需要小工具或网页时用 save_app 生成为快应用，用户可一键启动。" +
		"重要的用户偏好与事实可写入长期记忆（结构见 <memory> 块）。")
	if st.SystemExtra != "" {
		b.WriteString("\n\n" + st.SystemExtra)
	}
	b.WriteString("\n\n<memory>\n" +
		"# 长期记忆\n" +
		"- 根目录 memory/（工作目录相对），分三个区：\n" +
		"  - memory/longterm/ —— 长期记忆：harness.md 是索引（下方已加载，" +
		"可直接用文件工具更新），主题文件按需创建，不进上下文，用 findstr/grep 检索\n" +
		"  - memory/skills/ —— 能力记忆：沉淀的技能\n" +
		"  - sessions/ —— 话题存档：历史会话全文（compact 后的旧库）\n" +
		"# 索引（harness.md）\n" +
		hooks.EnsureHarnessMd(ctx, fsys) +
		"\n</memory>")
	if skills, err := skill.LoadDir(ctx, fsys, hooks.SkillsDir); err == nil && len(skills) > 0 {
		b.WriteString("\n\n<skills>\n（仅名称与描述；使用前先调用 load_skill 获取完整指令与脚本路径）")
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

/* mcpStatusList 返回状态栏 MCP 清单（描述前 8 字）。 */
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
