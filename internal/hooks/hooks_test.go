package hooks

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/localsession"
	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/types"
)

/* memFS 内存文件系统（测试用）。 */
type memFS map[string][]byte

func (m memFS) Read(_ context.Context, p string) ([]byte, error) {
	if d, ok := m[p]; ok {
		return d, nil
	}
	return nil, os.ErrNotExist
}
func (m memFS) Write(_ context.Context, p string, data []byte) error {
	m[p] = append([]byte(nil), data...)
	return nil
}
func (m memFS) List(_ context.Context, dir string) ([]fs.Entry, error) { return nil, nil }
func (m memFS) Edit(_ context.Context, p, oldText, newText string) (int, error) {
	d, ok := m[p]
	if !ok {
		return 0, os.ErrNotExist
	}
	n := strings.Count(string(d), oldText)
	if n == 0 {
		return 0, fmt.Errorf("not found in %s", p)
	}
	m[p] = []byte(strings.ReplaceAll(string(d), oldText, newText))
	return n, nil
}

/* fakeProvider 返回固定摘要文本。 */
type fakeProvider struct{ reply string }

func (f fakeProvider) Name() string { return "fake" }
func (f fakeProvider) Invoke(_ context.Context, req *types.ModelRequest) (*types.ModelResponse, error) {
	return &types.ModelResponse{Content: f.reply}, nil
}

func newTestState(msgs []types.Message) *types.LoopState {
	return &types.LoopState{Messages: msgs, Tools: types.NewToolRegistry()}
}

func TestRotateTruncationKeepsProtocol(t *testing.T) {
	fsys := memFS{}
	sess := localsession.New(fsys, "old-session")
	topics := NewTopics(fsys)
	r := NewRotate(fakeProvider{"这是一份交接摘要"}, fsys, sess, topics, 0, nil)

	state := newTestState([]types.Message{
		{Role: types.RoleUser, Content: "聊聊 Go 并发"},
		{Role: types.RoleAssistant, Content: "goroutine 是…"},
		{Role: types.RoleUser, Content: "换个话题，聊聊数据库"},
		{
			Role: types.RoleAssistant,
			ToolCalls: []types.ToolCall{
				{ID: "call-1", Name: RotateTool, Args: []byte(`{"reason":"用户换话题"}`)},
			},
		},
	})

	action, err := r.OnToolStart(context.Background(), state, &types.ToolCall{
		ID: "call-1", Name: RotateTool, Args: []byte(`{"reason":"用户换话题"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ezhook.KindSkip {
		t.Fatalf("expect skip, got %v", action.Kind)
	}

	// 截断协议：2 条 = [交接摘要, 原末条 assistant(tool_calls)]
	if len(state.Messages) != 2 {
		t.Fatalf("expect 2 messages after rotate, got %d", len(state.Messages))
	}
	if state.Messages[0].Role != types.RoleAssistant || state.Messages[0].Content == "" {
		t.Fatal("first message should be handover summary")
	}
	last := state.Messages[1]
	if last.Role != types.RoleAssistant || len(last.ToolCalls) != 1 || last.ToolCalls[0].ID != "call-1" {
		t.Fatal("last message must keep the tool_calls pairing")
	}

	// session 已切换
	if sess.ID() == "old-session" || sess.ID() == "" {
		t.Fatalf("session id should rotate, got %q", sess.ID())
	}

	// 索引落盘：旧话题入档
	list := topics.Load()
	if len(list) != 1 || list[0].ID != "old-session" || list[0].Title != "聊聊 Go 并发" {
		t.Fatalf("topic index wrong: %+v", list)
	}
}

func TestRotateForkRejected(t *testing.T) {
	fsys := memFS{}
	r := NewRotate(fakeProvider{}, fsys, localsession.New(fsys, "s"), NewTopics(fsys), 0, nil)
	state := newTestState([]types.Message{{Role: types.RoleUser, Content: "x"}})
	state.ForkID = "task-1"
	action, err := r.OnToolStart(context.Background(), state, &types.ToolCall{
		ID: "c", Name: RotateTool,
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ezhook.KindSkip {
		t.Fatal("fork rotate must be skipped")
	}
}

func TestTopicsMatch(t *testing.T) {
	tp := NewTopics(memFS{})
	_ = tp.Add(TopicEntry{ID: "a1", Title: "Go 并发", Summary: "goroutine"})
	_ = tp.Add(TopicEntry{ID: "b2", Title: "数据库", Summary: "postgres"})
	if got := len(tp.Match("")); got != 2 {
		t.Fatalf("match all = %d", got)
	}
	if got := len(tp.Match("a1")); got != 1 {
		t.Fatalf("match id prefix = %d", got)
	}
	if got := len(tp.Match("数据库")); got != 1 {
		t.Fatalf("match title = %d", got)
	}
}

func TestMemoryInject(t *testing.T) {
	fsys := memFS{MemoryFile: []byte("用户偏好简洁回答")}
	m := NewMemory(fsys)
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "base prompt"},
		{Role: types.RoleUser, Content: "hi"},
	})
	if err := m.OnStart(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 2 || state.Messages[0].Role != types.RoleSystem {
		t.Fatal("memory must keep single system message")
	}
	if state.Messages[0].Content != "base prompt\n\n# 长期记忆（用户与你的沉淀，可用文件工具更新 memory.md）\n用户偏好简洁回答" {
		t.Fatalf("injected content wrong: %q", state.Messages[0].Content)
	}
}
