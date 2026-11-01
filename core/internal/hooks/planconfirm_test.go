package hooks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/types"
)

/* planCall 构造一次 plan 工具调用。 */
func planCall(id, args string) *types.ToolCall {
	return &types.ToolCall{ID: id, Name: "plan", Args: json.RawMessage(args)}
}

/*
runPlanCall 在 goroutine 里跑 OnToolStart，稍等等待者登记后再投递决策
（决策先于等待到达会被当过期丢弃，与线上语义一致）。
*/
func runPlanCall(t *testing.T, h *PlanConfirm, ch chan<- PlanDecision, call *types.ToolCall, d PlanDecision) (hook.Action, error) {
	t.Helper()
	type res struct {
		act hook.Action
		err error
	}
	done := make(chan res, 1)
	go func() {
		act, err := h.OnToolStart(context.Background(), newTestState(nil), call)
		done <- res{act, err}
	}()
	time.Sleep(20 * time.Millisecond) // 等 await 登记
	ch <- d
	select {
	case r := <-done:
		return r.act, r.err
	case <-time.After(2 * time.Second):
		t.Fatal("OnToolStart 未在 2s 内返回")
		return hook.Action{}, nil
	}
}

/* TestPlanConfirmProceeds：确认后参数保持原样，调用继续执行。 */
func TestPlanConfirmProceeds(t *testing.T) {
	h, ch := NewPlanConfirm()
	args := `{"action":"new","goal":"目标","approach":"方案","progress":[{"title":"a","status":"pending"},{"title":"b","status":"pending"}]}`
	call := planCall("c1", args)
	act, err := runPlanCall(t, h, ch, call, PlanDecision{CallID: "c1", Action: PlanActionConfirm})
	if err != nil {
		t.Fatalf("OnToolStart: %v", err)
	}
	if act.Kind != hook.KindProceed {
		t.Fatalf("expect proceed, got %v", act.Kind)
	}
	if string(call.Args) != args {
		t.Fatalf("confirm must not rewrite args, got %s", call.Args)
	}
}

/* TestPlanConfirmFeedback：反馈短路调用，意见进工具结果。 */
func TestPlanConfirmFeedback(t *testing.T) {
	h, ch := NewPlanConfirm()
	call := planCall("c2", `{"action":"new","goal":"目标","approach":"方案"}`)
	act, err := runPlanCall(t, h, ch, call, PlanDecision{CallID: "c2", Action: PlanActionFeedback, Feedback: "步骤太粗"})
	if err != nil {
		t.Fatalf("OnToolStart: %v", err)
	}
	if act.Kind != hook.KindSkip {
		t.Fatalf("expect skip, got %v", act.Kind)
	}
	if !strings.Contains(act.Result, "步骤太粗") || !strings.Contains(act.Result, "重新提交") {
		t.Fatalf("skip result = %q", act.Result)
	}
	if string(call.Args) != `{"action":"new","goal":"目标","approach":"方案"}` {
		t.Fatalf("feedback must not rewrite args, got %s", call.Args)
	}
}

/* TestPlanConfirmIgnoresOtherCalls：update/clear 与其它工具一律放行。 */
func TestPlanConfirmIgnoresOtherCalls(t *testing.T) {
	h, _ := NewPlanConfirm()
	for _, c := range []*types.ToolCall{
		planCall("c3", `{"action":"update","progress":[]}`),
		{ID: "c4", Name: "terminal", Args: json.RawMessage(`{"command":"ls"}`)},
	} {
		act, err := h.OnToolStart(context.Background(), newTestState(nil), c)
		if err != nil || act.Kind != hook.KindProceed {
			t.Fatalf("%s/%s: act=%v err=%v", c.Name, c.ID, act, err)
		}
	}
}

/* TestPlanConfirmCtxCancel：用户停止轮次时等待立刻结束。 */
func TestPlanConfirmCtxCancel(t *testing.T) {
	h, _ := NewPlanConfirm()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	act, err := h.OnToolStart(ctx, newTestState(nil), planCall("c5", `{"action":"new","goal":"目标","approach":"方案"}`))
	if err == nil {
		t.Fatalf("expect ctx error")
	}
	if act.Kind != hook.KindSkip {
		t.Fatalf("expect skip on cancel, got %v", act.Kind)
	}
}
