package hooks

import (
	"context"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/types"
)

/* 同参重复第 4 次标记待提醒，OnModelStart 消费时插消息且仅一条；不同参数各算各的。 */
func TestLoopGuardRepeat(t *testing.T) {
	ctx := context.Background()
	h := NewLoopGuard()
	state := newTestState([]types.Message{{Role: types.RoleSystem, Content: "base"}})
	call := &types.ToolCall{ID: "c1", Name: "browser_action", Args: []byte(`{"action":"scroll"}`)}
	tick := func() { // 一次迭代：工具调用 → 模型调用前消费提醒
		_, _ = h.OnToolStart(ctx, state, call)
		if err := h.OnModelStart(ctx, state); err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 3; i++ {
		tick()
	}
	if n := countGuard(state.Messages); n != 0 {
		t.Fatalf("below threshold must not warn, got %d", n)
	}
	for i := 0; i < 10; i++ { // 第 4 次触发后继续重复：仍只一条
		tick()
	}
	if n := countGuard(state.Messages); n != 1 {
		t.Fatalf("warn exactly once, got %d", n)
	}
	if !strings.Contains(lastGuard(state.Messages), "browser_action") {
		t.Fatalf("warn must name the tool: %q", lastGuard(state.Messages))
	}
	// 提醒消息只能出现在消息序列末尾（OnModelStart 时机），不得夹在 tool_use 与结果之间
	if state.Messages[len(state.Messages)-1].Content != lastGuard(state.Messages) {
		t.Fatal("guard message must be appended at sequence end")
	}

	// 不同参数各算各的
	other := &types.ToolCall{ID: "c2", Name: "browser_action", Args: []byte(`{"action":"click"}`)}
	for i := 0; i < 3; i++ {
		_, _ = h.OnToolStart(ctx, state, other)
	}
	if err := h.OnModelStart(ctx, state); err != nil {
		t.Fatal(err)
	}
	if n := countGuard(state.Messages); n != 1 {
		t.Fatalf("different args count separately, got %d", n)
	}

	if err := h.OnEnd(ctx, state); err != nil {
		t.Fatal(err)
	}
}

func countGuard(msgs []types.Message) int {
	n := 0
	for _, m := range msgs {
		if strings.HasPrefix(m.Content, "<"+LoopGuardTag+">") {
			n++
		}
	}
	return n
}

func lastGuard(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.HasPrefix(msgs[i].Content, "<"+LoopGuardTag+">") {
			return msgs[i].Content
		}
	}
	return ""
}
