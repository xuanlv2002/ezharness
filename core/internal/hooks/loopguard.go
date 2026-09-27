/*
loopguard 是循环护栏 hook：轮内检测同一工具同一参数的反复调用（死循环、
无限重试的典型形态），达到阈值在下一次模型调用前插一条 <loop_guard>
提醒消息——模型自省收敛；同时发 loop.guard 事件（前端时间线实时小字条），
用户同见、可随时停止本轮。

插入时机必须是 OnModelStart（消息序列稳定：tool_result 已全部 append）：
OnToolStart 时 append 会把消息夹在 assistant 的 tool_use 与 tool_result
之间，anthropic 协议要求两者紧邻，插中间直接 400。只提醒不拦截（拦截
会跟 approve 的拒绝语义打架），同一调用组合只提醒一次。
*/
package hooks

import (
	"context"
	"fmt"
	"sync"

	"github.com/xuanlv2002/ezloop/event"
	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/types"
)

/* LoopGuardTag 是循环护栏提醒的包裹标签。 */
const LoopGuardTag = "loop_guard"

/* EventLoopGuard 是护栏触发事件（Data 为 LoopGuardData，前端时间线实时渲染）。 */
const EventLoopGuard = event.EventType("loop.guard")

/* LoopGuardData 是护栏触发载荷。 */
type LoopGuardData struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
}

/* guardRepeat 是同一调用组合的提醒阈值（出现次数达到即提醒一次）。 */
const guardRepeat = 4

/* LoopGuard 实现循环护栏。 */
type LoopGuard struct {
	mu    sync.Mutex
	count map[*types.LoopState]map[string]int
	pend  map[*types.LoopState]LoopGuardData // 达阈值的待提醒项（OnModelStart 消费）
}

/* NewLoopGuard 创建护栏 hook。 */
func NewLoopGuard() *LoopGuard {
	return &LoopGuard{
		count: map[*types.LoopState]map[string]int{},
		pend:  map[*types.LoopState]LoopGuardData{},
	}
}

func (h *LoopGuard) Name() string { return "loopguard" }

/* OnToolStart 只计数与标记，不插消息（插入时机见 OnModelStart）。 */
func (h *LoopGuard) OnToolStart(_ context.Context, state *types.LoopState, call *types.ToolCall) (ezhook.Action, error) {
	key := call.Name + "\x00" + string(call.Args)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count[state] == nil {
		h.count[state] = map[string]int{}
	}
	h.count[state][key]++
	if h.count[state][key] == guardRepeat {
		h.pend[state] = LoopGuardData{Tool: call.Name, Count: guardRepeat}
	}
	return ezhook.Proceed, nil
}

/* OnModelStart 消费待提醒项：消息序列稳定处插入提醒消息并发事件。 */
func (h *LoopGuard) OnModelStart(_ context.Context, state *types.LoopState) error {
	h.mu.Lock()
	d, ok := h.pend[state]
	delete(h.pend, state)
	h.mu.Unlock()
	if !ok {
		return nil
	}
	msg := "<" + LoopGuardTag + ">\n（系统护栏）" + fmt.Sprintf("%s 已用相同参数调用 %d 次，多半此路不通。"+
		"停止重复：读返回结果确认状态，换手段，或 ask_user 向用户说明并请示（用户也可直接停止本轮）。\n</%s>",
		d.Tool, d.Count, LoopGuardTag)
	state.Messages = append(state.Messages, types.Message{Role: types.RoleUser, Content: msg})
	state.EmitEvent(EventLoopGuard, d)
	return nil
}

/* OnEnd 清计数（Run 结束即释放）。 */
func (h *LoopGuard) OnEnd(_ context.Context, state *types.LoopState) error {
	h.mu.Lock()
	delete(h.count, state)
	delete(h.pend, state)
	h.mu.Unlock()
	return nil
}
