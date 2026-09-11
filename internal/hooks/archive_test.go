package hooks

import (
	"context"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/types"
)

/*
归档换代：旧库封存、新库初始快照（空 messages + 新 system + compress
边）、话题线换代 LeafID 指新库。视图（含 marker）作摘要输入。
*/
func TestArchiveSession(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	store := NewStore(fsys, "old-session")
	sys := NewSysPrompt("base prompt", "")
	topics := NewTopics(fsys)
	trace := NewTrace(fsys, store, nil)
	store.BindSys(sys, "fake")
	store.SetLineRoot("old-session")

	// 旧库先落一份全量（idle 归档前提：文件已是最新）
	oldSnap := &SessionSnap{ID: "old-session", CreatedAt: 100,
		Messages: []types.Message{
			{Role: types.RoleUser, Content: "聊聊 Go 并发"},
			{Role: types.RoleAssistant, Content: "goroutine 是…"},
		}}
	if err := SaveSnap(ctx, fsys, oldSnap); err != nil {
		t.Fatal(err)
	}

	view := []types.Message{
		{Role: types.RoleUser, Content: "<context_trim>\n摘要：早期已整理\n</context_trim>"},
		{Role: types.RoleUser, Content: "近期问题"},
	}
	info, err := ArchiveSession(ctx, fakeProvider{"交接摘要"}, fsys, store, sys, topics, trace,
		func() string { return "rebuilt base" }, oldSnap.Messages, view)
	if err != nil {
		t.Fatal(err)
	}
	if info.OldID != "old-session" || info.NewID == "" || info.Title != "聊聊 Go 并发" {
		t.Fatalf("info wrong: %+v", info)
	}

	// 旧库封存（消息原样保留）
	old, err := LoadSnap(ctx, fsys, "old-session")
	if err != nil || !old.Archived || len(old.Messages) != 2 {
		t.Fatalf("old session must be archived intact: %+v err=%v", old, err)
	}
	// 新库初始快照：空 messages、新 system、compress 边
	ns, err := LoadSnap(ctx, fsys, info.NewID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns.Messages) != 0 || !strings.Contains(ns.SystemPrompt, "交接摘要") ||
		!strings.Contains(ns.SystemPrompt, "rebuilt base") {
		t.Fatalf("new snapshot wrong: msgs=%d sys=%.80s", len(ns.Messages), ns.SystemPrompt)
	}
	if ns.TargetID != "old-session" || ns.SeedKind != "compress" {
		t.Fatalf("new snapshot edge wrong: %+v", ns)
	}
	if ns.LineRoot != "old-session" {
		t.Fatalf("new snapshot keeps line root, got %q", ns.LineRoot)
	}
	// session 名称独立于线标题：新代未命名起步，落盘时按本代首条 user 命名
	if ns.Title != "" {
		t.Fatalf("new generation starts untitled, got %q", ns.Title)
	}
	if got := store.Title(); got != "" {
		t.Fatalf("store title must reset on rotate, got %q", got)
	}
	// 模拟新代首条对话后落盘：Title=该条 user（线标题不受影响）
	state2 := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "rebuilt base"},
		{Role: types.RoleUser, Content: "归档后的新问题"},
	})
	if err := store.OnEnd(context.Background(), state2); err != nil {
		t.Fatal(err)
	}
	ns2, err := LoadSnap(ctx, fsys, info.NewID)
	if err != nil {
		t.Fatal(err)
	}
	if ns2.Title != "归档后的新问题" {
		t.Fatalf("session must be named by its first real user msg, got %q", ns2.Title)
	}
	// 内存切换：SetID/SetPrev、sys 热更
	if store.ID() != info.NewID {
		t.Fatalf("store must switch to new id, got %q", store.ID())
	}
	if _, summary := sys.Parts(); !strings.Contains(summary, "交接摘要") {
		t.Fatalf("sys must pin summary block, got %q", summary)
	}
	// 话题线换代：LeafID 指新库，线标题保持身份不变（归档未创建新分支）
	list := topics.Load()
	if len(list) != 1 || list[0].LeafID != info.NewID || list[0].Title != "聊聊 Go 并发" {
		t.Fatalf("line must rotate leaf keeping title: %+v", list)
	}
}

/*
trim 落盘全量：折叠段经 Store.ArchivePrefix 进档案，OnEnd 拼接后
session.json 含 marker 前全量；两次连续 trim 拼接顺序正确。
*/
func TestTrimArchiveFullOnDisk(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	store := NewStore(fsys, "s1")
	sys := NewSysPrompt("base", "")
	store.BindSys(sys, "fake")
	tr := NewTrim(fakeProvider{"摘要"}, nil, 100, 1000)

	// 第一轮：q1..a3 两次问答后整理（q1,a1 折叠，marker+q2..a3 保留）→ 落盘
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "base"},
		{Role: types.RoleUser, Content: "q1"}, {Role: types.RoleAssistant, Content: "a1"},
		{Role: types.RoleUser, Content: "q2"}, {Role: types.RoleAssistant, Content: "a2"},
		{Role: types.RoleUser, Content: "q3"}, {Role: types.RoleAssistant, Content: "a3"},
	})
	state.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 999}}
	if err := tr.OnLoop(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err := store.OnEnd(ctx, state); err != nil {
		t.Fatal(err)
	}

	// 第二轮：重启恢复（快照全量 → ViewStart 起的视图）+ 新消息 → 再整理 → 再落盘
	snap1, err := LoadSnap(ctx, fsys, "s1")
	if err != nil {
		t.Fatal(err)
	}
	view := snap1.Messages[ViewStart(snap1.Messages):]
	state2 := newTestState(append([]types.Message{{Role: types.RoleSystem, Content: "base"}},
		append(append([]types.Message{}, view...),
			types.Message{Role: types.RoleUser, Content: "q4"},
			types.Message{Role: types.RoleAssistant, Content: "a4"},
			types.Message{Role: types.RoleUser, Content: "q5"},
			types.Message{Role: types.RoleAssistant, Content: "a5"},
		)...))
	state2.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 999}}
	if err := tr.OnLoop(ctx, state2); err != nil {
		t.Fatal(err)
	}
	if err := store.OnEnd(ctx, state2); err != nil {
		t.Fatal(err)
	}

	snap, err := LoadSnap(ctx, fsys, "s1")
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, m := range snap.Messages {
		sb.WriteString(m.Content + ",")
	}
	got := sb.String()
	// 全量顺序：q1,a1（第一轮折叠）→ marker+q2..a3（第二轮折叠，首个 marker 随段链式折叠）→ 剩余
	if !strings.HasPrefix(got, "q1,a1,") {
		t.Fatalf("archive order wrong (early first):\n got %s", got)
	}
	for _, want := range []string{"q2", "a3", "q4", "q5", "<" + TrimTag + " kept="} {
		if !strings.Contains(got, want) {
			t.Fatalf("archive must keep %q: %s", want, got)
		}
	}
}
