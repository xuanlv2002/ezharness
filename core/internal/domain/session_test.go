package domain

import (
	"encoding/json"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/hooks"
)

/* fakeModelProvider 立即返回固定回复（无工具调用，一轮即完成）。 */
type fakeModelProvider struct{}

func (fakeModelProvider) Name() string { return "fake" }
func (fakeModelProvider) Invoke(_ context.Context, _ *types.ModelRequest) (*types.ModelResponse, error) {
	return &types.ModelResponse{Content: "ok"}, nil
}

/* StartRun 不得死锁：模型视图过滤须在会话锁内安全取用（重入锁即卡死发消息）。 */
func TestStartRunNoDeadlock(t *testing.T) {
	s := &Session{}
	s.setHistory([]types.Message{
		{Role: types.RoleSystem, Content: "sys"},
		{Role: types.RoleUser, Content: "q1"},
		{Role: types.RoleUser, Content: "<context_trim>\n摘要\n</context_trim>"},
		{Role: types.RoleUser, Content: "q2"},
	})
	agent := core.NewAgent(fakeModelProvider{})
	s.Attach(Wiring{Agent: agent})

	done := make(chan struct{})
	go func() {
		defer close(done)
		h, _, err := s.StartRun(context.Background(), "hello", nil)
		if err != nil {
			t.Error(err)
			return
		}
		state, waitErr := h.Wait()
		s.FinishRun(state, waitErr)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("StartRun deadlocked (modelView re-entering session lock)")
	}
}

func mkMsgs(n int) []types.Message {
	out := make([]types.Message, 0, n)
	for i := 0; i < n; i++ {
		role := types.RoleUser
		if i%2 == 1 {
			role = types.RoleAssistant
		}
		out = append(out, types.Message{Role: role, Content: "m"})
	}
	return out
}

/* modelView：最后 marker 的 kept 回溯保留段 + marker + 之后；无 marker 全量。 */
func TestModelViewFromLastMarker(t *testing.T) {
	s := &Session{}
	s.setHistory([]types.Message{
		{Role: types.RoleUser, Content: "f1"},
		{Role: types.RoleUser, Content: "f2"},
		{Role: types.RoleUser, Content: "<context_trim kept=\"2\">\n摘要：早期\n</context_trim>"},
		{Role: types.RoleUser, Content: "t1"},
		{Role: types.RoleUser, Content: "<context_trim kept=\"1\">\n摘要：近期\n</context_trim>"},
		{Role: types.RoleUser, Content: "q3"},
	})
	view := s.ModelView()
	// 最后 marker kept=1 → [t1, marker, q3]
	if len(view) != 3 || view[0].Content != "t1" || !hooks.IsTrimMarker(view[1]) || view[2].Content != "q3" {
		t.Fatalf("view = kept seg + marker + after: %+v", view)
	}
	// 渲染视图仍全量
	if len(s.History()) != 6 {
		t.Fatal("history must keep full for rendering")
	}

	s2 := &Session{}
	s2.setHistory(mkMsgs(4))
	if len(s2.ModelView()) != 4 {
		t.Fatal("no marker = full view")
	}
}

/* FinishRun：trim 轮的折叠段与 marker 前档案合并，跨轮不丢。 */
func TestFinishRunMergesTrimmedArchive(t *testing.T) {
	s := &Session{}
	s.setHistory([]types.Message{
		{Role: types.RoleSystem, Content: "sys"},
		{Role: types.RoleUser, Content: "q1"},
		{Role: types.RoleUser, Content: "<context_trim>\n摘要\n</context_trim>"},
		{Role: types.RoleUser, Content: "q2"},
	})
	// 本轮：视图从 marker 起，又 trim 折叠了 [marker,q2]，保留新 marker + q3
	state := &types.LoopState{Metadata: map[string]any{}}
	state.Messages = []types.Message{
		{Role: types.RoleSystem, Content: "sys"},
		{Role: types.RoleUser, Content: "<context_trim>\n摘要2\n</context_trim>"},
		{Role: types.RoleUser, Content: "q3"},
	}
	state.Metadata["trim_folded"] = []types.Message{
		{Role: types.RoleUser, Content: "<context_trim>\n摘要\n</context_trim>"},
		{Role: types.RoleUser, Content: "q2"},
	}
	s.FinishRun(state, nil)
	var got []types.Message
	for _, m := range s.History() { // 渲染/落盘视角不含 system
		if m.Role != types.RoleSystem {
			got = append(got, m)
		}
	}
	// 追加式档案：q1（上轮档案）+ marker,q2（本轮折叠原文）+ 新 marker + q3
	if len(got) != 5 || got[0].Content != "q1" || got[2].Content != "q2" ||
		!hooks.IsTrimMarker(got[3]) || got[4].Content != "q3" {
		var sb strings.Builder
		for _, m := range got {
			sb.WriteString(string(m.Role) + ":" + m.Content + " | ")
		}
		t.Fatalf("history wrong: %s", sb.String())
	}
}

/* 流式快照回放：增量帧累积成 stream.snapshot 进回放缓存，model_end 落定后
丢弃（切回分支/断线重连时 in-flight 内容靠它重建）。 */
func TestStreamSnapshotReplay(t *testing.T) {
	s := &Session{
		subs:    map[chan []byte]struct{}{},
		pending: map[string]Event{},
		snapAcc: map[string]*StreamSnapshot{},
		snapIdx: map[string]int{},
		snapFlushed: map[string]int{},
	}
	publish := func(typ string, data any) {
		s.Publish(Event{Type: typ, Ts: time.Now().UnixMilli(), Data: Raw(data)})
	}

	publish("loop_start", "hi")
	publish("model_chunk", "你")
	publish("model_chunk", "好")
	publish("reasoning_chunk", "想一想")
	s.cur = &runState{} // 回放走 turnFrames 的前提是轮在进行中

	var frame Event
	found := false
	for _, b := range s.ReplayFrames() {
		var e Event
		if json.Unmarshal(b, &e) == nil && e.Type == "stream.snapshot" {
			frame, found = e, true
		}
	}
	if !found {
		t.Fatal("stream.snapshot must be in replay frames")
	}
	var snap StreamSnapshot
	if err := json.Unmarshal(frame.Data, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Content != "你好" || snap.Reasoning != "想一想" {
		t.Fatalf("snapshot: %+v", snap)
	}

	// 构造期工具增量也进快照；model_end 后快照让位聚合帧
	publish("tool_chunk", map[string]any{"index": 0, "nameDelta": "write_", "argsDelta": `{"path"`})
	publish("model_end", ModelEndData{Content: "你好", Reasoning: "想一想"})
	for _, b := range s.ReplayFrames() {
		var e Event
		if json.Unmarshal(b, &e) == nil && e.Type == "stream.snapshot" {
			t.Fatal("model_end 后快照帧应丢弃")
		}
	}
}
