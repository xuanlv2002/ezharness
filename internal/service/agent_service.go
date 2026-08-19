/*
AgentService 负责 agent 的装配与重建：provider、hooks、warp、工具集。
工具集是 ezharness 的产品决策（read/write/edit/bash 四件，见 internal/tools）。
*/
package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/ext/hook/contextfix"
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
	"ezharness/internal/tools"
)

/* AgentService 装配领域会话的运行时。 */
type AgentService struct {
	Hub *domain.Hub
}

/* Assemble 按设置装配 agent 并注入会话。 */
func (a *AgentService) Assemble(s *domain.Session, st domain.Settings) {
	ctx := context.Background()

	skillHook, err := skill.NewFromFS(ctx, s.Fsys, "skills")
	if err != nil {
		skillHook = skill.New()
	}

	provider := openai.New(openai.Options{
		BaseURL: st.BaseURL,
		APIKey:  s.Cfg.APIKey,
		Model:   st.Model,
	})

	approver, approveCh := approve.New(a.needsApprove)
	asker, answerCh := askuser.New()
	planner, planCh := taskplan.New()
	rotator := hooks.NewRotate(provider, s.Fsys, s.Sess, s.Topics, st.RotateThreshold,
		func(info hooks.RotateInfo) {
			s.SetIdentity(info.NewID) // 宿主侧同步 session 标识
		})
	recaller := hooks.NewRecall(s.Fsys, s.Topics)

	agent := core.NewAgent(provider,
		core.WithSystemPrompt(systemPrompt(st)),
		core.WithModelWarp(modelretry.Warp()),
		core.WithToolWarp(limit.Warp(4), safetool.Warp()),
		core.WithTools(tools.All(s.Fsys, st.Shell)...),
		core.WithHooks(
			contextfix.New(),
			offload.New(s.Fsys),
			skillHook,
			hooks.NewMemory(s.Fsys),
			approver,
			asker,
			planner,
			task.New(),
			rotator,
			recaller,
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
			"read_file", "write_file", "edit_file", "bash",
			askuser.ToolName, taskplan.ToolName, task.ToolName,
			hooks.RotateTool, hooks.RecallTool, "mcp_router",
		},
	})
}

/* Reassemble 重建活动会话的 agent（设置变更后，需空闲）。 */
func (a *AgentService) Reassemble(st domain.Settings) error {
	s := a.Hub.Active
	if s.Busy() {
		return domain.ErrBusy
	}
	a.Assemble(s, st)
	return nil
}

/* needsApprove 参数级免审白名单：只读工具与人机交互工具免审。 */
func (a *AgentService) needsApprove(c *types.ToolCall) bool {
	switch c.Name {
	case "read_file", askuser.ToolName, taskplan.ToolName, task.ToolName,
		hooks.RotateTool, hooks.RecallTool:
		return false
	case "bash":
		var args struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(c.Args, &args)
		for _, p := range []string{"ls", "cat", "head", "tail", "pwd",
			"git status", "git diff", "git log", "go test"} {
			if args.Command == p || strings.HasPrefix(args.Command, p+" ") {
				return false
			}
		}
	}
	return true
}

/* systemPrompt 组装系统提示（长期记忆由 memory hook 每轮注入）。 */
func systemPrompt(st domain.Settings) string {
	p := "你是 ezharness——一个持续陪伴用户的设备级 agent，可全权操作本机文件与命令。" +
		"能用工具就用工具，回答简洁。可并行的子任务用 task 分身去做。" +
		"用户想换话题时用 rotate_context 归档旧话题开启新会话；" +
		"用户提起之前聊过的内容时用 recall_topic 回顾存档；" +
		"重要的用户偏好与事实可写入 memory.md 长期记住。"
	if st.SystemExtra != "" {
		p += "\n\n" + st.SystemExtra
	}
	return p
}
