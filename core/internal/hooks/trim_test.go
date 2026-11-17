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
	return NewTrim(fakeProvider{reply}, nil, threshold, 1000, nil)
}

/* 水位自动路径：OnLoop 就地折叠（立即生效），OnEnd 在 end_reason 后落位 marker。 */
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
		t.Fatalf("handed-over = fold minus kept exchanges (2), got %d", len(FoldedOf(state)))
	}
	// OnLoop 只截断（立即生效）：[system, 两次输入的完整交互]，无 marker
	if len(state.Messages) != 5 {
		t.Fatalf("expect [system,2 exchanges] before OnEnd, got %d: %+v", len(state.Messages), state.Messages)
	}
	if state.Messages[1].Content != "早期问题 2" || state.Messages[4].Content != "近期回答" {
		t.Fatalf("kept exchanges must be intact: %+v", state.Messages)
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

	// 引擎行为模拟：remind.OnEnd 先追加 end_reason，trim.OnEnd 落位 marker
	state.AppendMessage(types.Message{Role: types.RoleUser, Content: "<end_reason>\n收尾\n</end_reason>"})
	if err := tr.OnEnd(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 7 {
		t.Fatalf("expect [system,2 exchanges,end_reason,marker], got %d: %+v", len(state.Messages), state.Messages)
	}
	marker := state.Messages[6]
	if marker.Role != types.RoleUser || !IsTrimMarker(marker) ||
		!strings.Contains(marker.Content, "整理摘要") || !strings.Contains(marker.Content, `kept="1"`) {
		t.Fatalf("marker wrong: %+v", marker)
	}
	// 落位即消费：再跑 OnEnd 不重复追加
	if err := tr.OnEnd(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 7 {
		t.Fatalf("OnEnd must consume the stash, got %d msgs", len(state.Messages))
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
两次输入恰好填满保留区（kept=1 保留两次）：保留区之下无可折叠，
报错并标记本轮已整理（水位路径不逐迭代重试）。
*/
func TestTrimNothingToFold(t *testing.T) {
	tr := newTrimForTest("s", 500)
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "b"},
		{Role: types.RoleUser, Content: "q1"},
		{Role: types.RoleAssistant, Content: "a1"},
		{Role: types.RoleUser, Content: "q2"},
		{Role: types.RoleAssistant, Content: "a2"},
	})
	state.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 800}}
	if err := tr.OnLoop(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 5 || len(FoldedOf(state)) != 0 {
		t.Fatalf("two exchanges = kept region, nothing folded: %+v", state.Messages)
	}
	if state.Metadata["trimmed"] != true {
		t.Fatal("nothing-to-fold must still mark trimmed (no per-iteration retry)")
	}
}

/*
工具路径（排队制）：OnToolStart 在并发回调区只登记，不写 state；
Skip 结果入史后 OnLoop（串行区）统一截断——trim 调用对随触发交互
完整保留，marker 尾插，协议完整。
*/
func TestTrimToolPathQueued(t *testing.T) {
	tr := newTrimForTest("工具摘要", 0) // 水位禁用：仅排队路径生效
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "b"},
		{Role: types.RoleUser, Content: "q0"},
		{Role: types.RoleAssistant, Content: "a0"},
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
	if len(state.Messages) != 7 || len(FoldedOf(state)) != 0 {
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

	// OnLoop 串行区消费排队：截断为 [system, q1 交互 + q2 触发交互(含调用对)]
	if err := tr.OnLoop(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 1+5 {
		t.Fatalf("expect [system,2 exchanges] before OnEnd, got %d: %+v", len(state.Messages), state.Messages)
	}
	var pairOK bool
	for _, m := range state.Messages[1:6] {
		if m.Role == types.RoleAssistant && len(m.ToolCalls) == 1 {
			pairOK = true
		}
	}
	if !pairOK {
		t.Fatalf("tail must keep trim call pair: %+v", state.Messages)
	}
	if len(FoldedOf(state)) != 1+1 { // q0/a0 折叠；q1 起的两次交互保留
		t.Fatalf("folded = q0 exchange only (2), got %d", len(FoldedOf(state)))
	}

	// OnEnd（end_reason 之后）落位 marker：本轮最下方，时间序自然
	state.AppendMessage(types.Message{Role: types.RoleUser, Content: "<end_reason>\n收尾\n</end_reason>"})
	if err := tr.OnEnd(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 8 || !IsTrimMarker(state.Messages[7]) ||
		!strings.Contains(state.Messages[7].Content, "工具摘要") {
		t.Fatalf("marker must land after end_reason at turn end: %+v", state.Messages)
	}
}

/* fork：折叠 SeedLen 之后的增量，seed 完整保留，marker 尾插。 */
func TestTrimForkSeedLen(t *testing.T) {
	tr := newTrimForTest("fork 摘要", 500)
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "seed system"},
		{Role: types.RoleUser, Content: "主上下文（归主库）"},
		{Role: types.RoleUser, Content: "分身任务 1"},
		{Role: types.RoleAssistant, Content: "分身过程 1"},
		{Role: types.RoleUser, Content: "分身任务 2"},
		{Role: types.RoleAssistant, Content: "分身过程 2"},
		{Role: types.RoleUser, Content: "分身任务 3"},
		{Role: types.RoleAssistant, Content: "分身过程 3"},
	})
	state.ForkID = "task-1"
	state.SeedLen = 2
	state.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 999}}

	if err := tr.OnLoop(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	// 完整 seed（system+主上下文）不动；增量首个交互折叠，保留两次交互
	if state.Messages[0].Content != "seed system" || state.Messages[1].Content != "主上下文（归主库）" {
		t.Fatalf("fork trim must keep full seed: %+v", state.Messages[:2])
	}
	if len(state.Messages) != 6 || state.Messages[2].Content != "分身任务 2" || IsTrimMarker(state.Messages[5]) {
		t.Fatalf("expect [seed 2 msgs, 2 exchanges] before OnEnd, got %+v", state.Messages)
	}
	// SeedLen 不变（seed 完整保留，剥离逻辑按 seed 边界存增量）
	if state.SeedLen != 2 {
		t.Fatalf("SeedLen must stay (seed intact), got %d", state.SeedLen)
	}
	if len(FoldedOf(state)) != 2 {
		t.Fatalf("fork fold = first fork exchange (2), got %d", len(FoldedOf(state)))
	}
	// fork 不记 end_reason，OnEnd 直接落位 marker 在增量末尾
	if err := tr.OnEnd(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 7 || !IsTrimMarker(state.Messages[6]) {
		t.Fatalf("fork marker must land at increment tail: %+v", state.Messages)
	}
}

/* ViewStart：从最后 marker 向上回溯 kept+1 次完整交互定视图起点；无 marker 全量。 */
func TestViewStartFromLastMarker(t *testing.T) {
	mk := func(kept string) types.Message {
		return types.Message{Role: types.RoleUser, Content: "<context_trim kept=\"" + kept + "\">…</context_trim>"}
	}
	hist := []types.Message{
		{Role: types.RoleSystem, Content: "sys"},
		{Role: types.RoleUser, Content: "f1"}, // 1 早期档案
		mk("1"),                               // 2 marker1
		{Role: types.RoleUser, Content: "t1"}, // 3 marker2 之上保留的交互
		{Role: types.RoleUser, Content: "t2"}, // 4 触发 marker2 整理的交互
		mk("1"),                               // 5 marker2（触发点尾插）
		{Role: types.RoleUser, Content: "q3"}, // 6 marker2 之后新增
	}
	// 视图 = marker2 向上 kept=1 → 两次交互：起点 3（t1 的轮起点）
	if s := ViewStart(hist); s != 3 {
		t.Fatalf("expect view start 3, got %d", s)
	}
	// 无 marker：全量
	if s := ViewStart(hist[:2]); s != 0 {
		t.Fatalf("no marker = 0, got %d", s)
	}
}

/* groupStart：按真实 user 输入定界，吞并开场注入；不足 keep+1 次返回 0。 */
func TestGroupStart(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: "q1"},
		{Role: types.RoleAssistant, Content: "a1"},
		{Role: types.RoleUser, Content: "<end_reason>\n收尾\n</end_reason>"},
		{Role: types.RoleUser, Content: "<agent_status>\n快照\n</agent_status>"},
		{Role: types.RoleUser, Content: "q2"},
		{Role: types.RoleAssistant, Content: "a2"},
		{Role: types.RoleUser, Content: "<loop_guard>\n护栏\n</loop_guard>"},
		{Role: types.RoleUser, Content: "q3"},
		{Role: types.RoleAssistant, Content: "a3"},
	}
	// keep=1：数到 q2（第 2 条真实输入），吞并其上的 agent_status，
	// 起点落在 end_reason 之后（一次完整交互的边界）
	if s := groupStart(msgs, len(msgs), 1); s != 3 {
		t.Fatalf("expect start 3 (after end_reason), got %d", s)
	}
	// keep=0：只保留触发这次（q3），其上的 loop_guard 不是开场注入
	if s := groupStart(msgs, len(msgs), 0); s != 7 {
		t.Fatalf("expect start 7 (q3 itself), got %d", s)
	}
	// 输入不足 keep+1 次：无可折叠
	if s := groupStart(msgs[:6], 6, 2); s != 0 {
		t.Fatalf("insufficient inputs = 0, got %d", s)
	}
}
