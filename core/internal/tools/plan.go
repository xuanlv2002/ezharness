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
			"复杂任务（≥3 个实质步骤）开始时用 action=new 提交方案，三段字段：\n"+
			"goal：目标——要达成什么，一句话（≤200 字）\n"+
			"approach：方案——采用的做法、选择理由与关键取舍（10–2000 字，不写工具名流水账）\n"+
			"progress：步骤——2–12 步，title ≤60 字、动词开头、不带序号、不重复；status=pending|running|completed，最多一个 running。"+
			"要求具体可执行，不复述用户原话、不写寒暄。"+
			"new 提交后阻塞等用户确认（计划卡只读展示三段，用户可确认或提交反馈）；反馈会作为工具结果返回，按意见修改后重新 new；"+
			"确认前计划不会落盘，也不要开始执行。"+
			"action=update：完成或切换一步就提交，progress 必须传完整列表（整表替换），goal/approach 可缺省（保留原值）。"+
			"action=clear：计划被放弃或不再适用时清除；任务完成保留全部 completed 即可，不必 clear。"+
			"一两步的简单任务不要用",
		func(ctx context.Context, in *planArgs) (string, error) {
			switch in.Action {
			case "new":
				goal := strings.TrimSpace(in.Goal)
				approach := strings.TrimSpace(in.Approach)
				if goal == "" || approach == "" {
					return "", errors.New("new 需要 goal 与 approach：目标一句话，方案写清做法与取舍")
				}
				if err := validateText(goal, approach); err != nil {
					return "", err
				}
				progress, err := normalizeProgress(in.Progress)
				if err != nil {
					return "", err
				}
				p := &hooks.SessionPlan{Goal: goal, Approach: approach, Progress: progress}
				if err := hooks.SavePlan(ctx, fsys, rootID, p); err != nil {
					return "", err
				}
				if onUpdate != nil {
					onUpdate(p)
				}
				return planSummary(p), nil
			case "update":
				goal := strings.TrimSpace(in.Goal)
				approach := strings.TrimSpace(in.Approach)
				hasText := goal != "" || approach != ""
				hasProgress := in.Progress != nil
				if !hasText && !hasProgress {
					return "", errors.New("update 需要 goal、approach 或 progress")
				}
				if hasText {
					if err := validateText(goal, approach); err != nil {
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
				if approach != "" {
					current.Approach = approach
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
	Goal     string             `json:"goal,omitempty" desc:"目标：要达成什么，一句话（new 必填，≤200 字；update 可选，缺省保留原值）"`
	Approach string             `json:"approach,omitempty" desc:"方案：采用的做法、选择理由与关键取舍（new 必填，10–2000 字；update 可选，缺省保留原值）"`
	Progress []planProgressArgs `json:"progress,omitempty" desc:"步骤清单（2–12 步；new 必填，update 整表替换；最多一个 running）"`
}

/* 计划字段规范（超出范围直接报错回给模型自纠）。 */
const (
	goalMaxRunes     = 200
	approachMinRunes = 10
	approachMaxRunes = 2000
	planMinSteps     = 2
	planMaxSteps     = 12
	stepMaxRunes     = 60
)

/* validateText 校验目标/方案长度（空串=不修改，跳过；new 的必填在调用处判）。 */
func validateText(goal, approach string) error {
	if n := len([]rune(goal)); n > goalMaxRunes {
		return fmt.Errorf("goal 太长（%d 字，上限 %d）：目标一句话即可", n, goalMaxRunes)
	}
	if n := len([]rune(approach)); n > 0 && n < approachMinRunes {
		return fmt.Errorf("approach 太短（%d 字，至少 %d 字）：写清做法与取舍", n, approachMinRunes)
	} else if n > approachMaxRunes {
		return fmt.Errorf("approach 太长（%d 字，上限 %d）：保留关键信息", n, approachMaxRunes)
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
	return s
}
