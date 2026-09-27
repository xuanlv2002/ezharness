/*
loopguard 是循环护栏 hook：轮内检测同一工具同一参数的反复调用（死循环、
无限重试的典型形态），达到阈值插一条 <loop_guard> 提醒消息——模型自省
收敛，用户在时间线同见、可随时停止本轮。只提醒不拦截（拦截会跟 approve
的拒绝语义打架），同一调用组合只提醒一次。
*/
package hooks

import (
	"context"
	"fmt"
	"sync"

	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/types"
)

/* LoopGuardTag 是循环护栏提醒的包裹标签。 */
const LoopGuardTag = "loop_guard"

/* guardRepeat 是同一调用组合的提醒阈值（出现次数达到即提醒一次）。 */
const guardRepeat = 4

/* LoopGuard 实现循环护栏。 */
type LoopGuard struct {
	mu    sync.Mutex
	count map[*types.LoopState]map[string]int
}

/* NewLoopGuard 创建护栏 hook。 */
func NewLoopGuard() *LoopGuard {
	return &LoopGuard{count: map[*types.LoopState]map[string]int{}}
}

func (h *LoopGuard) Name() string { return "loopguard" }

func (h *LoopGuard) OnToolStart(_ context.Context, state *types.LoopState, call *types.ToolCall) (ezhook.Action, error) {
	key := call.Name + "\x00" + string(call.Args)
	h.mu.Lock()
	if h.count[state] == nil {
		h.count[state] = map[string]int{}
	}
	h.count[state][key]++
	n := h.count[state][key]
	h.mu.Unlock()
	if n != guardRepeat {
		return ezhook.Proceed, nil
	}
	msg := "<" + LoopGuardTag + ">\n（系统护栏）" + fmt.Sprintf("%s 已用相同参数调用 %d 次，多半此路不通。"+
		"停止重复：读返回结果确认状态，换手段，或 ask_user 向用户说明并请示（用户也可直接停止本轮）。\n</%s>",
		call.Name, guardRepeat, LoopGuardTag)
	state.Messages = append(state.Messages, types.Message{Role: types.RoleUser, Content: msg})
	return ezhook.Proceed, nil
}

/* OnEnd 清计数（Run 结束即释放）。 */
func (h *LoopGuard) OnEnd(_ context.Context, state *types.LoopState) error {
	h.mu.Lock()
	delete(h.count, state)
	h.mu.Unlock()
	return nil
}
