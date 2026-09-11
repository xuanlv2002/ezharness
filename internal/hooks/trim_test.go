package hooks

import (
	"context"
	"strings"
	"testing"

	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/types"
)

/* newTrimForTest 建 Trim；折叠段从 state.Metadata 断言。 */
func newTrimForTest(reply string, threshold int) *Trim {
	return NewTrim(fakeProvider{reply}, nil, threshold, 1000)
}

/* 水位自动路径：OnLoop 触发就地折叠，marker 尾插衔接，折叠段移交档案。 */
func TestTrimAutoByWatermark(t *testing.T) {
	tr := newTrimForTest("整理摘要", 500)
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "base"},
		{Role: types.RoleUser, Content: "早期问题 1"},
		{Role: types.RoleAssistant, Content: "早期回答 1"},
		{Role: types.RoleUser, Content: "早期问题 2"},
		{Role: types.RoleAssistant, Content: "早期回答 2"},
		{Role: types.RoleUser, Content: "近期问题"},
		{Role: types.RoleAssistant, Content: "近期回答"},
	})
	state.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 800}}

	if err := tr.OnLoop(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(FoldedOf(state)) != 2 {
		t.Fatalf("handed-over = fold minus tail (2), got %d", len(FoldedOf(state)))
	}
	// [system, tail 4, marker]：marker 尾插（时间序自然），立即生效
	if len(state.Messages) != 6 {
		t.Fatalf("expect [system,4 tail,marker], got %d: %+v", len(state.Messages), state.Messages)
	}
	marker := state.Messages[5]
	if marker.Role != types.RoleUser || !IsTrimMarker(marker) ||
		!strings.Contains(marker.Content, "整理摘要") || !strings.Contains(marker.Content, `kept="4"`) {
		t.Fatalf("marker wrong: %+v", marker)
	}
	if state.Messages[1].Content != "早期问题 2" || state.Messages[4].Content != "近期回答" {
		t.Fatalf("tail must keep recent messages: %+v", state.Messages)
	}
	if state.Metadata["trimmed"] != true {
		t.Fatal("must set trimmed flag for in-turn dedup")
	}

	// 防重入：同轮再次超水位不再折叠
	before := len(state.Messages)
	state.LastResponse.Usage.PromptTokens = 900
	if err := tr.OnLoop(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != before || len(FoldedOf(state)) != 2 {
		t.Fatalf("second trim in same turn must be skipped, msgs=%d folded=%d",
			len(state.Messages), len(FoldedOf(state)))
	}
}

/* 低于阈值不触发；threshold<=0 禁用。 */
func TestTrimDisabledBelowThreshold(t *testing.T) {
	tr := newTrimForTest("s", 500)
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "b"},
		{Role: types.RoleUser, Content: "q"},
		{Role: types.RoleAssistant, Content: "a"},
	})
	state.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 100}}
	if err := tr.OnLoop(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 3 || len(FoldedOf(state)) != 0 {
		t.Fatal("below threshold must not trim")
	}

	tr2 := newTrimForTest("s", 0) // 禁用
	tr2.OnLoop(context.Background(), state)
	if len(state.Messages) != 3 {
		t.Fatal("threshold<=0 must disable auto trim")
	}
}

/*
工具路径（排队制）：OnToolStart 在并发回调区只登记，不写 state；
Skip 结果入史后 OnLoop（串行区）统一截断——trim 调用对保留在 tail，
marker 尾插，协议完整。
*/
func TestTrimToolPathQueued(t *testing.T) {
	tr := newTrimForTest("工具摘要", 0) // 水位禁用：仅排队路径生效
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "b"},
		{Role: types.RoleUser, Content: "q1"},
		{Role: types.RoleAssistant, Content: "a1"},
		{Role: types.RoleUser, Content: "q2"},
		{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "c1", Name: TrimTool}}},
	})
	action, err := tr.OnToolStart(context.Background(), state, &types.ToolCall{ID: "c1", Name: TrimTool})
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ezhook.KindSkip || action.Result == "" {
		t.Fatalf("expect skip with message, got %+v", action)
	}
	// 排队期零改动（并发回调区不得写 state）
	if len(state.Messages) != 5 || len(FoldedOf(state)) != 0 {
		t.Fatalf("queued trim must not touch state: %+v", state.Messages)
	}
	// 同轮重复调用：提示已排队
	again, _ := tr.OnToolStart(context.Background(), state, &types.ToolCall{ID: "c2", Name: TrimTool})
	if again.Kind != ezhook.KindSkip || !strings.Contains(again.Result, "已安排") {
		t.Fatalf("duplicate call must be rejected: %+v", again)
	}

	// 引擎行为模拟：Skip 结果入史（协议完整对）
	state.Messages = append(state.Messages, types.Message{Role: types.RoleTool, ToolCallID: "c1", Content: action.Result})
	state.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 10}}

	// OnLoop 串行区消费排队：截断为 [system, tail 4(含调用对), marker]
	if err := tr.OnLoop(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 1+4+1 {
		t.Fatalf("expect [system,4 tail,marker], got %d: %+v", len(state.Messages), state.Messages)
	}
	var pairOK, markerOK bool
	for _, m := range state.Messages {
		if m.Role == types.RoleAssistant && len(m.ToolCalls) == 1 {
			pairOK = true
		}
		if IsTrimMarker(m) {
			markerOK = strings.Contains(m.Content, "工具摘要")
		}
	}
	if !pairOK || !markerOK {
		t.Fatalf("tail must keep trim call pair, marker last: %+v", state.Messages)
	}
	if !IsTrimMarker(state.Messages[len(state.Messages)-1]) {
		t.Fatal("marker must be the last message (chronological tail)")
	}
	if len(FoldedOf(state)) != 1 { // 仅 q1 折叠；a1,q2 与调用对保留
		t.Fatalf("folded = q1 only (1), got %d", len(FoldedOf(state)))
	}
}

/* fork：折叠 SeedLen 之后的增量，seed 完整保留，marker 尾插。 */
func TestTrimForkSeedLen(t *testing.T) {
	tr := newTrimForTest("fork 摘要", 500)
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "seed system"},
		{Role: types.RoleUser, Content: "主上下文（归主库）"},
		{Role: types.RoleUser, Content: "分身任务"},
		{Role: types.RoleAssistant, Content: "分身过程 1"},
		{Role: types.RoleAssistant, Content: "分身过程 2"},
		{Role: types.RoleAssistant, Content: "分身过程 3"},
	})
	state.ForkID = "task-1"
	state.SeedLen = 2
	state.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 999}}

	if err := tr.OnLoop(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	// 完整 seed（system+主上下文）不动；增量 4 条 ≤ K 全折叠，marker 尾插
	if state.Messages[0].Content != "seed system" || state.Messages[1].Content != "主上下文（归主库）" {
		t.Fatalf("fork trim must keep full seed: %+v", state.Messages[:2])
	}
	if len(state.Messages) != 3 || !IsTrimMarker(state.Messages[2]) {
		t.Fatalf("expect [seed 2 msgs, marker], got %+v", state.Messages)
	}
	// SeedLen 不变（seed 完整保留，剥离逻辑按 seed 边界存增量）
	if state.SeedLen != 2 {
		t.Fatalf("SeedLen must stay (seed intact), got %d", state.SeedLen)
	}
	if len(FoldedOf(state)) != 4 {
		t.Fatalf("fork fold = seed 后全部增量 (4), got %d", len(FoldedOf(state)))
	}
}

/* ViewStart：最后 marker 的 kept 回溯保留段，clamp 不跨上一 marker。 */
func TestViewStartFromKept(t *testing.T) {
	mk := func(kept string) types.Message {
		return types.Message{Role: types.RoleUser, Content: "<context_trim kept=\"" + kept + "\">…</context_trim>"}
	}
	hist := []types.Message{
		{Role: types.RoleSystem, Content: "sys"},
		{Role: types.RoleUser, Content: "f1"},   // 1 早期档案
		{Role: types.RoleUser, Content: "f2"},   // 2 早期档案
		{Role: types.RoleUser, Content: "m5"},   // 3 marker1 保留段
		{Role: types.RoleUser, Content: "m6"},   // 4 marker1 保留段
		mk("2"),                                 // 5 marker1（kept=2 → 保留段 3-4）
		{Role: types.RoleUser, Content: "new1"}, // 6 marker2 保留段
		mk("1"),                                 // 7 marker2（kept=1 → 保留段 6）
		{Role: types.RoleUser, Content: "t3"},   // 8 新增
	}
	// 视图 = marker2 前 kept=1 条 + marker2 + 之后 = [new1, marker2, t3]
	if s := ViewStart(hist); s != 6 {
		t.Fatalf("expect view start 6, got %d", s)
	}
	// 无 marker：全量
	if s := ViewStart(hist[:3]); s != 0 {
		t.Fatalf("no marker = 0, got %d", s)
	}
	// kept 越界（早于上一 marker）：clamp 到上一 marker 之后
	odd := []types.Message{
		{Role: types.RoleUser, Content: "a"},
		mk("2"),
		{Role: types.RoleUser, Content: "b"},
		mk("5"), // kept=5 越过 marker1
		{Role: types.RoleUser, Content: "c"},
	}
	if s := ViewStart(odd); s != 2 { // clamp 到 marker1(索引1) 之后
		t.Fatalf("expect clamp to 2, got %d", s)
	}
}

/* tailStart：孤儿 tool 的配对 assistant 被截在窗口外时，起点前扩纳入配对。 */
func TestTailStartExpandsForOrphanTool(t *testing.T) {
	fold := []types.Message{
		{Role: types.RoleUser, Content: "q"},
		{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "1"}}},
		{Role: types.RoleTool, ToolCallID: "1"}, // 末窗口若含它则孤儿，前扩纳入 A1
		{Role: types.RoleUser, Content: "x"},
		{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "2"}}},
		{Role: types.RoleTool, ToolCallID: "2"},
	}
	// 末 2 条无孤儿：直接保留
	if s := tailStart(fold, 2); s != 4 {
		t.Fatalf("expect tail start 4, got %d", s)
	}
	// 末 4 条含 T1（配对 A1 在窗口外）：前扩到 A1，保留连续后缀
	if s := tailStart(fold, 4); s != 1 {
		t.Fatalf("expect tail start 1 (expand for pairing), got %d", s)
	}
	// 段不长于 K：全折叠
	if s := tailStart(fold, 6); s != 6 {
		t.Fatalf("short fold must collapse entirely, got %d", s)
	}
}
