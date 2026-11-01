/*
planconfirm 让新计划先经用户确认：plan action=new 时发 plan.request 事件
并阻塞，用户在时间线计划卡里确认，或提交反馈要求重做。

与 approve/askuser 同构：阻塞在 channel 上，中断/恢复对引擎透明。确认即
原样放行，工具照常校验落盘；反馈则短路本次调用，把意见作为工具结果交回
模型。装配位置排在 approver 之后（与 askuser 同序）：用户给 plan 开了审批
时先走通用审批，拒绝即短路，计划卡不会再出现。
*/
package hooks

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/types"
)

/* EventPlanRequest 是计划确认请求事件（Data 为 *types.ToolCall，前端解析 Args）。 */
const EventPlanRequest = event.EventType("plan.request")

/* 计划确认处置动作。 */
const (
	PlanActionConfirm  = "confirm"  // 按原方案落盘执行
	PlanActionFeedback = "feedback" // 不执行，按反馈修改后重新提交
)

/* planToolName 是计划工具名（tools 包依赖 hooks，这里不能反向引用）。 */
const planToolName = "plan"

/* PlanDecision 是对一次计划确认请求的回应。CallID 留空视为回应当前请求。 */
type PlanDecision struct {
	CallID   string
	Action   string // confirm | feedback
	Feedback string // feedback：用户意见，原样交回模型
}

/* PlanConfirm 是计划确认 hook。 */
type PlanConfirm struct {
	mu      sync.Mutex
	waiters map[string]chan PlanDecision
}

/*
NewPlanConfirm 创建 hook 并返回决策 channel 的发送端。
决策必须从其他 goroutine 发送（与 approve.New 同理），同步发送会死锁。
*/
func NewPlanConfirm() (*PlanConfirm, chan<- PlanDecision) {
	ch := make(chan PlanDecision)
	h := &PlanConfirm{waiters: map[string]chan PlanDecision{}}
	go h.route(ch)
	return h, ch
}

func (h *PlanConfirm) Name() string { return "planconfirm" }

/* OnToolStart 只拦 plan 的 new 动作；其余调用原样放行。 */
func (h *PlanConfirm) OnToolStart(ctx context.Context, state *types.LoopState, call *types.ToolCall) (hook.Action, error) {
	if call.Name != planToolName || !isPlanNew(call.Args) {
		return hook.Proceed, nil
	}
	state.EmitEvent(EventPlanRequest, call)
	d, ok := h.await(ctx, call.ID)
	if !ok {
		return hook.Skip(""), ctx.Err()
	}
	return applyPlanDecision(d), nil
}

/* isPlanNew 判定是不是要确认的新计划（参数解析失败交给工具自己报错）。 */
func isPlanNew(args json.RawMessage) bool {
	var a struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(args, &a) != nil {
		return false
	}
	return a.Action == "new"
}

/* applyPlanDecision 按用户处置放行（confirm）或短路调用（feedback）。 */
func applyPlanDecision(d PlanDecision) hook.Action {
	switch d.Action {
	case PlanActionConfirm:
		return hook.Proceed
	case PlanActionFeedback:
		msg := "用户没有确认该计划"
		if fb := strings.TrimSpace(d.Feedback); fb != "" {
			msg += "，反馈：" + fb
		}
		return hook.Skip(msg + "。请按反馈修改后用 plan action=new 重新提交；未确认前不要执行计划内容。")
	}
	return hook.Skip("用户没有确认该计划，计划不会执行；请先与用户确认方向再重新提交。")
}

/* await 阻塞取本 call 的决策；ctx 取消返回 false。 */
func (h *PlanConfirm) await(ctx context.Context, callID string) (PlanDecision, bool) {
	ch := make(chan PlanDecision, 1)
	h.mu.Lock()
	h.waiters[callID] = ch
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.waiters, callID)
		h.mu.Unlock()
	}()
	select {
	case d := <-ch:
		return d, true
	case <-ctx.Done():
		return PlanDecision{}, false
	}
}

/* route 把共享 channel 上收到的决策按 CallID 投给对应等待者。 */
func (h *PlanConfirm) route(ch <-chan PlanDecision) {
	for d := range ch {
		h.mu.Lock()
		w := h.waiters[d.CallID]
		if w == nil && d.CallID == "" && len(h.waiters) == 1 {
			for _, only := range h.waiters {
				w = only
			}
		}
		h.mu.Unlock()
		if w == nil {
			continue // 轮已结束或 callID 对不上：丢弃过期决策
		}
		select {
		case w <- d:
		default: // 同一请求已有决策在途
		}
	}
}
