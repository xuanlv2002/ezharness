/*
AgentService 负责 agent 的装配与重建：provider、hooks、warp、工具集。
工具集是 ezharness 的产品决策（read/write/edit/bash 四件，见 internal/tools）。

上下文机制保持最简（2026-08-22 用户定调）：只有对话与 session 存档，
不做压缩/卸载/轮换等扩展——上下文体系由用户后续专门设计，
internal/hooks 的 rotate/recall 仅保留代码不装配。
*/
package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
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
	"ezharness/internal/tools"
)

/* AgentService 装配领域会话的运行时。 */
type AgentService struct {
	Hub *domain.Hub
}

/* Assemble 按配置装配 agent 并注入会话（主模型取 models 四槽 main 启用条目）。 */
func (a *AgentService) Assemble(s *domain.Session, st domain.Settings) {
	ctx := context.Background()

	skillHook, err := skill.NewFromFS(ctx, s.Fsys, hooks.SkillsDir)
	if err != nil {
		skillHook = skill.New()
	}

	main := a.Hub.ModelsSnapshot().ActiveMain()
	if main == nil {
		main = &domain.ModelEntry{}
	}
	provider := openai.New(openai.Options{
		BaseURL: main.BaseURL,
		APIKey:  main.APIKey,
		Model:   main.Name,
	})

	approver, approveCh := approve.New(a.needsApprove)
	asker, answerCh := askuser.New()
	planner, planCh := taskplan.New()

	agent := core.NewAgent(provider,
		core.WithSystemPrompt(systemPrompt(st)),
		core.WithModelWarp(modelretry.Warp()),
		core.WithToolWarp(limit.Warp(4), safetool.Warp()),
		core.WithTools(tools.All(s.Fsys, st.Shell)...),
		core.WithHooks(
			skillHook,
			hooks.NewMemory(s.Fsys),
			approver,
			asker,
			planner,
			task.New(),
			NewMcpHook(s.Fsys),
			s.Sess,
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
		ToolNames: []string{
			"read_file", "write_file", "edit_file", "bash", "save_app",
			askuser.ToolName, taskplan.ToolName, task.ToolName,
			"mcp_router",
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

/* needsApprove 按审批策略判定（安全页四档）。
人机交互与内部工具恒免审；未知工具默认审批。运行时读设置快照，
改策略即时生效（无需重建 agent）。 */
func (a *AgentService) needsApprove(c *types.ToolCall) bool {
	switch c.Name {
	case askuser.ToolName, taskplan.ToolName:
		return false // 交互工具不属用户管控面
	}
	name := c.Name
	if name == "mcp_router" {
		name = "mcp.*" // ezloop mcp 是单一 router 工具，二段式（server/tool 在 args）
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

/* matchRuleList 判定工具调用是否命中名单：bash 匹配命令（相等或词边界前缀）、
文件工具匹配路径前缀、mcp 匹配 server 或 server.tool。 */
func matchRuleList(list []string, ruleTool string, args json.RawMessage) bool {
	key := ""
	switch ruleTool {
	case "bash":
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
		if ruleTool == "bash" && strings.HasPrefix(key, e+" ") {
			return true // 命令词边界
		}
		if ruleTool != "bash" && strings.HasPrefix(key, e) {
			return true // 路径 / server.tool 前缀
		}
	}
	return false
}

/* systemPrompt 组装系统提示（长期记忆由 memory hook 每轮注入）。 */
func systemPrompt(st domain.Settings) string {
	p := "你是 ezharness——一个持续陪伴用户的设备级 agent，可全权操作本机文件与命令。" +
		"能用工具就用工具，回答简洁。可并行的子任务用 task 分身去做。" +
		"用户需要小工具或网页时用 save_app 生成为快应用，用户可一键启动。" +
		"重要的用户偏好与事实可写入 memory/longterm/harness.md 长期记住，" +
		"更多记忆细节用 grep 在 memory/longterm/ 下检索。"
	if st.SystemExtra != "" {
		p += "\n\n" + st.SystemExtra
	}
	return p
}
