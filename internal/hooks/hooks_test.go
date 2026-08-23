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
func (m memFS) List(_ context.Context, dir string) ([]fs.Entry, error) {
	prefix := strings.TrimSuffix(dir, "/") + "/"
	seen := map[string]fs.Entry{}
	for p := range m {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := strings.TrimPrefix(p, prefix)
		parts := strings.Split(rest, "/")
		if len(parts) == 1 {
			seen[rest] = fs.Entry{Name: rest}
		} else {
			seen[parts[0]] = fs.Entry{Name: parts[0], IsDir: true}
		}
	}
	out := make([]fs.Entry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	return out, nil
}
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

	// 工具调用中途对上下文零改动：消息原样（模型在原上下文里汇总）
	if len(state.Messages) != 5 {
		t.Fatalf("expect untouched 5 messages after compact trigger, got %d", len(state.Messages))
	}
	last := state.Messages[4]
	if last.Role != types.RoleAssistant || len(last.ToolCalls) != 1 || last.ToolCalls[0].ID != "call-1" {
		t.Fatal("last message must keep the tool_calls pairing untouched")
	}

	// 挂起期不切库、不入档、system 不变：压缩轮还在进行
	if store.ID() != "old-session" {
		t.Fatalf("session must not switch before OnEnd, got %q", store.ID())
	}
	if list := topics.Load(); len(list) != 0 {
		t.Fatalf("topic index must be empty before OnEnd, got %+v", list)
	}
	if base, summary := sys.Parts(); base != "base prompt" || summary != "" {
		t.Fatalf("sysprompt must be untouched before OnEnd, got base=%q summary=%q", base, summary)
	}

	// 模拟剩余迭代：引擎追加工具结果与模型汇总（发生在原上下文里）
	state.Messages = append(state.Messages,
		types.Message{Role: types.RoleTool, ToolCallID: "call-1", Content: "上下文已压缩归档…"},
		types.Message{Role: types.RoleAssistant, Content: "好的，已重置。"},
	)

	// 轮末收尾
	if err := c.OnEnd(context.Background(), state); err != nil {
		t.Fatal(err)
	}

	// 新库切入；翻页后只剩 [新 system]（下一轮用户输入即新库第一条）
	if store.ID() == "old-session" || store.ID() == "" {
		t.Fatalf("session id should switch after OnEnd, got %q", store.ID())
	}
	if len(state.Messages) != 1 || state.Messages[0].Role != types.RoleSystem ||
		!strings.Contains(state.Messages[0].Content, "这是一份交接摘要") {
		t.Fatalf("expect [new system] after finish, got %+v", state.Messages)
	}
	// store.OnEnd 落盘＝新库初始快照：messages 空、新 system、prev 链（重启可恢复）
	if err := store.OnEnd(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	newSnap, err := LoadSnap(context.Background(), fsys, store.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(newSnap.Messages) != 0 {
		t.Fatalf("new session snapshot must be empty, got %d msgs", len(newSnap.Messages))
	}
	if !strings.Contains(newSnap.SystemPrompt, "这是一份交接摘要") {
		t.Fatalf("new snapshot must pin new system, got %q", newSnap.SystemPrompt)
	}
	if newSnap.PrevSession != "old-session" || newSnap.Archived {
		t.Fatalf("new snapshot prev chain wrong: %+v", newSnap)
	}

	// 旧库补写完整历史（含触发输入与过程消息）并封存
	snap, err := LoadSnap(context.Background(), fsys, "old-session")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Archived {
		t.Fatal("old session must be archived")
	}
	var sawTrigger, sawSummary bool
	for _, m := range snap.Messages {
		if m.Role == types.RoleUser && m.Content == "换个话题，聊聊数据库" {
			sawTrigger = true
		}
		if m.Role == types.RoleAssistant && m.Content == "好的，已重置。" {
			sawSummary = true
		}
	}
	if !sawTrigger || !sawSummary {
		t.Fatalf("old archive must contain trigger and wrap-up messages: %+v", snap.Messages)
	}
	if snap.SystemPrompt != "base prompt" {
		t.Fatalf("old archive must pin pre-compact system, got %q", snap.SystemPrompt)
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

/* 三层加载的第 2 层：load_skill 返回 SKILL.md 全文 + 路径 + 目录结构。 */
func TestSkillToolLoad(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	_ = fsys.Write(ctx, "memory/skills/pdf/SKILL.md",
		[]byte("---\nname: pdf\ndescription: 提取 PDF\n---\n\n# PDF 处理\n步骤：pdfplumber"))
	_ = fsys.Write(ctx, "memory/skills/pdf/scripts/extract.py", []byte("print(1)"))
	_ = fsys.Write(ctx, "memory/skills/pdf/references/api.md", []byte("api 文档"))

	h := NewSkillTool(fsys, "memory/skills")
	state := newTestState(nil)
	if err := h.OnStart(ctx, state); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Tools.Lookup(SkillTool); err != nil {
		t.Fatal("load_skill must be registered")
	}

	action, err := h.OnToolStart(ctx, state, &types.ToolCall{
		ID: "c1", Name: SkillTool, Args: []byte(`{"name":"pdf"}`),
	})
	if err != nil || action.Kind != ezhook.KindSkip {
		t.Fatalf("expect skip action, err=%v kind=%v", err, action.Kind)
	}
	r := action.Result
	if !strings.Contains(r, "memory/skills/pdf/SKILL.md") ||
		!strings.Contains(r, "步骤：pdfplumber") || // 全文（frontmatter 已剥离）
		!strings.Contains(r, "scripts/extract.py") || !strings.Contains(r, "references/api.md") {
		t.Fatalf("load result missing parts: %q", r)
	}
	if strings.Contains(r, "name: pdf") {
		t.Fatal("frontmatter must be stripped from instructions")
	}

	// 未知名：返回可用列表提示
	action, _ = h.OnToolStart(ctx, state, &types.ToolCall{
		ID: "c2", Name: SkillTool, Args: []byte(`{"name":"nope"}`),
	})
	if !strings.Contains(action.Result, "pdf") {
		t.Fatalf("unknown skill should list available: %q", action.Result)
	}
}
