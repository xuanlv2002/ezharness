package hooks

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/ext/fs"
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
	return &types.LoopState{Messages: msgs, Tools: types.NewToolRegistry(), Metadata: map[string]any{}}
}

func newCompactForTest(fsys memFS, reply string) (*Compact, *Store, *SysPrompt, *Topics) {
	store := NewStore(fsys, "old-session")
	sys := NewSysPrompt("base prompt", "")
	topics := NewTopics(fsys)
	trace := NewTrace(fsys, store, nil)
	store.BindSys(sys, "fake")
	c := NewCompact(fakeProvider{reply}, fsys, store, sys, topics, trace, 0,
		func() string { return "rebuilt base" }, nil)
	return c, store, sys, topics
}

func TestCompactTruncationKeepsProtocol(t *testing.T) {
	fsys := memFS{}
	c, store, sys, topics := newCompactForTest(fsys, "这是一份交接摘要")

	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "base prompt"},
		{Role: types.RoleUser, Content: "聊聊 Go 并发"},
		{Role: types.RoleAssistant, Content: "goroutine 是…"},
		{Role: types.RoleUser, Content: "换个话题，聊聊数据库"},
		{
			Role: types.RoleAssistant,
			ToolCalls: []types.ToolCall{
				{ID: "call-1", Name: CompactTool, Args: []byte(`{"reason":"用户换话题"}`)},
			},
		},
	})

	action, err := c.OnToolStart(context.Background(), state, &types.ToolCall{
		ID: "call-1", Name: CompactTool, Args: []byte(`{"reason":"用户换话题"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ezhook.KindSkip {
		t.Fatalf("expect skip, got %v", action.Kind)
	}

	// 截断协议：3 条 = [新 system(含摘要), handover, 原末条 assistant(tool_calls)]
	if len(state.Messages) != 3 {
		t.Fatalf("expect 3 messages after compact, got %d", len(state.Messages))
	}
	if state.Messages[0].Role != types.RoleSystem ||
		!strings.Contains(state.Messages[0].Content, "这是一份交接摘要") ||
		!strings.Contains(state.Messages[0].Content, "sessions/old-session") {
		t.Fatalf("new system must contain summary and prev path: %q", state.Messages[0].Content)
	}
	if state.Messages[1].Role != types.RoleAssistant {
		t.Fatal("second message should be handover")
	}
	last := state.Messages[2]
	if last.Role != types.RoleAssistant || len(last.ToolCalls) != 1 || last.ToolCalls[0].ID != "call-1" {
		t.Fatal("last message must keep the tool_calls pairing")
	}

	// session 已切换；SysPrompt 已重组（rebuildBase + 摘要段）
	if store.ID() == "old-session" || store.ID() == "" {
		t.Fatalf("session id should switch, got %q", store.ID())
	}
	base, summary := sys.Parts()
	if base != "rebuilt base" || !strings.Contains(summary, "这是一份交接摘要") {
		t.Fatalf("sysprompt should be rebuilt, got base=%q summary=%q", base, summary)
	}

	// 索引落盘：旧话题入档（含 Path/Kind）
	list := topics.Load()
	if len(list) != 1 || list[0].ID != "old-session" || list[0].Title != "聊聊 Go 并发" {
		t.Fatalf("topic index wrong: %+v", list)
	}
	if list[0].Path != "sessions/old-session" || list[0].Kind != "compact" {
		t.Fatalf("topic path/kind wrong: %+v", list[0])
	}
}

func TestCompactForkInPlace(t *testing.T) {
	fsys := memFS{}
	c, _, sys, _ := newCompactForTest(fsys, "fork 摘要")

	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "seed system"},
		{Role: types.RoleUser, Content: "主上下文"},
		{Role: types.RoleUser, Content: "分身任务指令"},
		{Role: types.RoleAssistant, Content: "分身过程…"},
	})
	state.ForkID = "task-1"
	state.SeedLen = 2

	action, err := c.OnToolStart(context.Background(), state, &types.ToolCall{
		ID: "c", Name: CompactTool,
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ezhook.KindSkip {
		t.Fatal("fork compact must be handled via skip")
	}

	// 就地截断：[system(含摘要), handover, 原末条]，SeedLen 重置
	if len(state.Messages) != 3 {
		t.Fatalf("expect 3 messages, got %d", len(state.Messages))
	}
	if !strings.Contains(state.Messages[0].Content, "fork 摘要") {
		t.Fatal("fork system must contain summary")
	}
	if state.SeedLen != 1 {
		t.Fatalf("SeedLen must reset to 1, got %d", state.SeedLen)
	}
	// 主会话 SysPrompt 不受 fork 压缩影响
	if _, summary := sys.Parts(); summary != "" {
		t.Fatal("main sysprompt must be untouched by fork compact")
	}
}

func TestKeepTail(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: "q"},
		{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "1"}}},
		{Role: types.RoleTool, ToolCallID: "1"},
	}
	// 工具路径：末条 assistant(tool_calls) 保留
	tail := keepTail([]types.Message{msgs[0], msgs[1]}, false)
	if len(tail) != 1 || tail[0].Role != types.RoleAssistant {
		t.Fatalf("tool path tail wrong: %+v", tail)
	}
	// 轮末路径：孤儿 tool 尾巴回退到最近的非 tool 条
	tail = keepTail(msgs, true)
	if len(tail) != 1 || tail[0].Role != types.RoleAssistant {
		t.Fatalf("auto path must drop orphan tool tail: %+v", tail)
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

func TestSysPromptSingleSystem(t *testing.T) {
	sys := NewSysPrompt("base", "summary")
	state := newTestState([]types.Message{{Role: types.RoleUser, Content: "hi"}})
	if err := sys.OnStart(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 2 || state.Messages[0].Role != types.RoleSystem {
		t.Fatal("sysprompt must prepend single system message")
	}
	if state.Messages[0].Content != "base\n\nsummary" {
		t.Fatalf("render wrong: %q", state.Messages[0].Content)
	}
}
