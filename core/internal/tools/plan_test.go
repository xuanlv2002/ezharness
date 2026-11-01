package tools

import (
	"strings"
	"testing"

	"ezharness/core/internal/hooks"
)

/* planSummary 在步骤全部完成时附带 clear 提示（完成当场递到模型面前）。 */
func TestPlanSummaryAllDoneHintsClear(t *testing.T) {
	p := &hooks.SessionPlan{Goal: "目标", Design: "方案", Progress: []hooks.PlanProgress{
		{Title: "一步", Status: hooks.PlanCompleted},
		{Title: "二步", Status: hooks.PlanCompleted},
	}}
	got := planSummary(p)
	if !strings.Contains(got, "plan action=clear") {
		t.Fatalf("全完成应提示 clear，得到：%s", got)
	}
}

func TestPlanSummaryPartialNoHint(t *testing.T) {
	p := &hooks.SessionPlan{Goal: "目标", Design: "方案", Progress: []hooks.PlanProgress{
		{Title: "一步", Status: hooks.PlanCompleted},
		{Title: "二步", Status: hooks.PlanRunning},
	}}
	if got := planSummary(p); strings.Contains(got, "plan action=clear") {
		t.Fatalf("未全部完成不应提示 clear，得到：%s", got)
	}
}
