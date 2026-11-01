package hooks

import (
	"strings"
	"testing"
)

/*
renderPlan 对全部完成的计划追加催促行（目标达成 clear / 未结束更新），
未全部完成不催——避免已完成计划滞留、每轮白占注入。
*/
func TestRenderPlanCompletionNudge(t *testing.T) {
	allDone := &SessionPlan{Goal: "目标", Progress: []PlanProgress{
		{Title: "一步", Status: PlanCompleted},
		{Title: "二步", Status: PlanCompleted},
	}}
	var b strings.Builder
	renderPlan(&b, allDone)
	if got := b.String(); !strings.Contains(got, "已全部完成但未清除") {
		t.Fatalf("全完成计划应附催促行，得到：\n%s", got)
	}

	part := &SessionPlan{Goal: "目标", Progress: []PlanProgress{
		{Title: "一步", Status: PlanCompleted},
		{Title: "二步", Status: PlanRunning},
	}}
	b.Reset()
	renderPlan(&b, part)
	if got := b.String(); strings.Contains(got, "已全部完成但未清除") {
		t.Fatalf("未全部完成不应催促，得到：\n%s", got)
	}
}
