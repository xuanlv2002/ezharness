/*
plan 工具：维护当前会话的任务计划与进度。计划落 sessions/<rootID>/plan.json，
每轮随 agent_status 注入模型；更新时同步推 plan.update 事件，前端在输入框
上方实时展示「正在做什么 + 完整计划」。
*/
package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/hooks"
	"ezharness/core/internal/osfs"
)

/*
Plan 返回计划工具。rootID 决定计划归属的分支；onUpdate 在计划变化后回调
（nil = 已清除），由装配层转成 SSE 事件。
*/
func Plan(fsys osfs.OS, rootID string, onUpdate func(*hooks.SessionPlan)) types.Tool {
	return types.NewTool("plan",
		"维护当前会话的任务计划与进度（输入框上方实时展示，并随每轮 agent_status 注入上下文）。"+
			"复杂任务（≥3 个实质步骤）开始时用 action=new 提交，五段字段：\n"+
			"goal：目标——要达成什么、做成什么样算完成，一句话（≤200 字）\n"+
			"context：背景——为什么做、现状与约束、明确不做什么划边界（10–1000 字）\n"+
			"design：方案——具体怎么做：分节写关键设计与取舍、列出涉及文件（30–4000 字，不写工具调用流水账）\n"+
			"progress：步骤——2–12 步，title ≤60 字、动词开头、不带序号、不重复；status=pending|running|completed，最多一个 running。\n"+
			"verify：验证——怎么确认做对了：跑什么构建/测试、实测什么场景（5–800 字）\n"+
			"标准是用户确认后照着做就能完成：方案写具体设计而不是口号，步骤可执行，验证可操作，不复述用户原话。"+
			"new 提交后阻塞等用户确认（计划卡只读展示五段，用户可确认或提交反馈）；反馈会作为工具结果返回，按意见修改后重新 new；"+
			"确认前计划不会落盘，也不要开始执行。"+
			"action=update：完成或切换一步就提交，progress 必须传完整列表（整表替换），goal/context/design/verify 可缺省（保留原值）。"+
			"action=clear：清除计划，两种情况必须调用——"+
			"一是目标已达成：把最后一步 update 成 completed 后随即 clear，不要保留已完成的计划；"+
			"二是计划失效：用户改向、当前请求与计划无关、或方案不再成立时，及时 clear 掉失效计划。"+
			"用户提出新的复杂任务时直接 new 提交新计划（覆盖旧计划），不必先 clear。"+
			"一两步的简单任务不要用",
		func(ctx context.Context, in *planArgs) (string, error) {
			switch in.Action {
			case "new":
				goal := strings.TrimSpace(in.Goal)
				background := strings.TrimSpace(in.Context)
				design := strings.TrimSpace(in.Design)
				verify := strings.TrimSpace(in.Verify)
				if goal == "" || background == "" || design == "" || verify == "" {
					return "", errors.New("new 需要 goal/context/design/verify 四段：目标一句话，背景划边界，方案写清怎么做，验证写清怎么确认")
				}
				if err := validateFields(goal, background, design, verify); err != nil {
					return "", err
				}
				progress, err := normalizeProgress(in.Progress)
				if err != nil {
					return "", err
				}
				p := &hooks.SessionPlan{Goal: goal, Context: background, Design: design, Progress: progress, Verify: verify}
				if err := hooks.SavePlan(ctx, fsys, rootID, p); err != nil {
					return "", err
				}
				if onUpdate != nil {
					onUpdate(p)
				}
				return planSummary(p), nil
			case "update":
				goal := strings.TrimSpace(in.Goal)
				background := strings.TrimSpace(in.Context)
				design := strings.TrimSpace(in.Design)
				verify := strings.TrimSpace(in.Verify)
				hasText := goal != "" || background != "" || design != "" || verify != ""
				hasProgress := in.Progress != nil
				if !hasText && !hasProgress {
					return "", errors.New("update 需要 goal、context、design、verify 或 progress")
				}
				if hasText {
					if err := validateFields(goal, background, design, verify); err != nil {
						return "", err
					}
				}
				current, err := hooks.LoadPlan(ctx, fsys, rootID)
				if err != nil {
					return "", err
				}
				if current == nil {
					current = &hooks.SessionPlan{}
				}
				if goal != "" {
					current.Goal = goal
				}
				if background != "" {
					current.Context = background
				}
				if design != "" {
					current.Design = design
				}
				if verify != "" {
					current.Verify = verify
				}
				if hasProgress {
					progress, err := normalizeProgress(in.Progress)
					if err != nil {
						return "", err
					}
					current.Progress = progress
				}
				if err := hooks.SavePlan(ctx, fsys, rootID, current); err != nil {
					return "", err
				}
				if onUpdate != nil {
					onUpdate(current)
				}
				return planSummary(current), nil
			case "clear":
				if err := hooks.SavePlan(ctx, fsys, rootID, &hooks.SessionPlan{}); err != nil {
					return "", err
				}
				if onUpdate != nil {
					onUpdate(nil)
				}
				return "计划已清除", nil
			}
			return "", fmt.Errorf("未知 action %q(new/update/clear)", in.Action)
		})
}

type planProgressArgs struct {
	Title  string `json:"title" desc:"步骤描述"`
	Status string `json:"status" enum:"pending|running|completed" desc:"pending=待执行;running=执行中;completed=已完成"`
}

type planArgs struct {
	Action   string             `json:"action" enum:"new|update|clear" desc:"new=提交新方案（等用户确认）;update=更新进度;clear=清除计划"`
	Goal     string             `json:"goal,omitempty" desc:"目标：要达成什么、做成什么样算完成，一句话（new 必填，≤200 字；update 可选，缺省保留原值）"`
	Context  string             `json:"context,omitempty" desc:"背景：为什么做、现状与约束、明确不做什么（new 必填，10–1000 字；update 可选，缺省保留原值）"`
	Design   string             `json:"design,omitempty" desc:"方案：分节写关键设计与取舍、列出涉及文件（new 必填，30–4000 字；update 可选，缺省保留原值）"`
	Progress []planProgressArgs `json:"progress,omitempty" desc:"步骤清单（2–12 步；new 必填，update 整表替换；最多一个 running）"`
	Verify   string             `json:"verify,omitempty" desc:"验证：怎么确认做对了——构建/测试/实测（new 必填，5–800 字；update 可选，缺省保留原值）"`
}

/* 计划字段规范（超出范围直接报错回给模型自纠）。 */
const (
	goalMaxRunes    = 200
	contextMinRunes = 10
	contextMaxRunes = 1000
	designMinRunes  = 30
	designMaxRunes  = 4000
	verifyMinRunes  = 5
	verifyMaxRunes  = 800
	planMinSteps    = 2
	planMaxSteps    = 12
	stepMaxRunes    = 60
)

/* validateFields 校验四段文本长度（空串=不修改，跳过；new 的必填在调用处判）。 */
func validateFields(goal, background, design, verify string) error {
	if n := len([]rune(goal)); n > goalMaxRunes {
		return fmt.Errorf("goal 太长（%d 字，上限 %d）：目标一句话即可", n, goalMaxRunes)
	}
	if err := validateRange("context", "背景", background, contextMinRunes, contextMaxRunes); err != nil {
		return err
	}
	if err := validateRange("design", "方案", design, designMinRunes, designMaxRunes); err != nil {
		return err
	}
	return validateRange("verify", "验证", verify, verifyMinRunes, verifyMaxRunes)
}

/* validateRange 校验单段长度（空串=不修改，跳过）。 */
func validateRange(field, label, s string, min, max int) error {
	if n := len([]rune(s)); n > 0 && n < min {
		return fmt.Errorf("%s 太短（%d 字，至少 %d 字）：%s要写实内容", field, n, min, label)
	} else if n > max {
		return fmt.Errorf("%s 太长（%d 字，上限 %d）：保留关键信息", field, n, max)
	}
	return nil
}

/*
normalizeProgress 校验并规范化步骤：2–12 步、标题单行且不重复、
status 只认三态、最多一个 running。
*/
func normalizeProgress(in []planProgressArgs) ([]hooks.PlanProgress, error) {
	if len(in) < planMinSteps || len(in) > planMaxSteps {
		return nil, fmt.Errorf("progress 需 %d–%d 步（当前 %d 步）", planMinSteps, planMaxSteps, len(in))
	}
	out := make([]hooks.PlanProgress, 0, len(in))
	seen := make(map[string]bool, len(in))
	running := 0
	for i, p := range in {
		title := strings.Join(strings.Fields(p.Title), " ")
		if title == "" {
			return nil, fmt.Errorf("progress[%d].title 不能为空", i)
		}
		if n := len([]rune(title)); n > stepMaxRunes {
			return nil, fmt.Errorf("progress[%d].title 太长（%d 字，上限 %d）", i, n, stepMaxRunes)
		}
		if seen[title] {
			return nil, fmt.Errorf("progress[%d].title 与前面步骤重复：%s", i, title)
		}
		seen[title] = true
		status := strings.TrimSpace(p.Status)
		switch status {
		case hooks.PlanPending, hooks.PlanRunning, hooks.PlanCompleted:
		default:
			return nil, fmt.Errorf("progress[%d].status 需为 pending/running/completed", i)
		}
		if status == hooks.PlanRunning {
			running++
		}
		out = append(out, hooks.PlanProgress{Title: title, Status: status})
	}
	if running > 1 {
		return nil, fmt.Errorf("最多一个步骤为 running（当前 %d 个）", running)
	}
	return out, nil
}

/* planSummary 返回一行工具结果（卡片正文由 plan.update 事件驱动，不重复整份 JSON）。 */
func planSummary(p *hooks.SessionPlan) string {
	done, running := 0, ""
	for _, step := range p.Progress {
		switch step.Status {
		case hooks.PlanCompleted:
			done++
		case hooks.PlanRunning:
			if running == "" {
				running = step.Title
			}
		}
	}
	s := fmt.Sprintf("计划已保存：%d/%d 步完成", done, len(p.Progress))
	if running != "" {
		s += "，正在：" + running
	}
	if len(p.Progress) > 0 && done == len(p.Progress) {
		s += "。全部步骤已完成：目标达成就调用 plan action=clear 清除计划；工作未结束则先更新计划"
	}
	return s
}
