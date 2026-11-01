package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/hooks"
)

/*
TreeLevel：根层按线最近活动分页，子代按创建时间倒序；fork 挂在分叉源
（compress 世代）的父级，根节点带 childCount。元数据读取不依赖 trace。
*/
func TestTreeLevelPaginatesRootsAndLoadsChildren(t *testing.T) {
	chdirTemp(t)
	h := newTestHub()
	ctx := context.Background()
	seed := func(id, title string, createdAt int64, target, kind, lineRoot string, msgs int) {
		snap := &hooks.SessionSnap{ID: id, Title: title, CreatedAt: createdAt,
			TargetID: target, SeedKind: kind, LineRoot: lineRoot}
		for i := 0; i < msgs; i++ {
			snap.Messages = append(snap.Messages, types.Message{Role: types.RoleUser, Content: "m"})
		}
		if err := hooks.SaveSnap(ctx, h.Fsys, snap); err != nil {
			t.Fatal(err)
		}
	}
	seed("A", "line A", 100, "", "new", "A", 2)
	seed("B", "line B", 200, "", "new", "B", 1)
	seed("C", "line C", 300, "", "new", "C", 0)
	seed("A2", "A 第二代", 110, "A", "compress", "A", 1)
	seed("F", "F fork", 120, "A2", "fork", "F", 0)
	for _, e := range []hooks.TopicEntry{
		{ID: "A", LeafID: "A", Title: "line A", CreatedAt: 100, UpdatedAt: 500},
		{ID: "B", LeafID: "B", Title: "line B", CreatedAt: 200, UpdatedAt: 400},
		{ID: "C", LeafID: "C", Title: "line C", CreatedAt: 300, UpdatedAt: 300},
		{ID: "F", LeafID: "F", Title: "F fork", CreatedAt: 120, UpdatedAt: 120, Kind: "fork"},
	} {
		if err := h.Topics.Add(e); err != nil {
			t.Fatal(err)
		}
	}

	svc := &TopicService{Hub: h}
	page := svc.TreeLevel(ctx, "", "", 2)
	if len(page.Nodes) != 2 || page.Nodes[0].ID != "A" || page.Nodes[1].ID != "B" {
		t.Fatalf("root page = %+v, want [A B]", page.Nodes)
	}
	if page.Total != 3 || page.Next == "" {
		t.Fatalf("page meta total=%d next=%q, want total 3 and next", page.Total, page.Next)
	}
	if page.Nodes[0].ChildCount != 2 {
		t.Fatalf("A childCount = %d, want 2 (compress + fork)", page.Nodes[0].ChildCount)
	}

	tail := svc.TreeLevel(ctx, "", page.Next, 2)
	if len(tail.Nodes) != 1 || tail.Nodes[0].ID != "C" || tail.Next != "" {
		t.Fatalf("tail page = %+v next=%q, want [C] and no next", tail.Nodes, tail.Next)
	}

	kids := svc.TreeLevel(ctx, "A", "", 0)
	if len(kids.Nodes) != 2 || kids.Nodes[0].ID != "F" || kids.Nodes[1].ID != "A2" {
		t.Fatalf("children = %+v, want [F A2] by createdAt desc", kids.Nodes)
	}
	for _, n := range kids.Nodes {
		if n.ParentID != "A" {
			t.Fatalf("%s parentId = %q, want A", n.ID, n.ParentID)
		}
	}
}

/* History tail 只回尾部并带绝对偏移，供前端锚点按 msgIdx 对位。 */
func TestHistoryTailReturnsStartOffset(t *testing.T) {
	chdirTemp(t)
	h := newTestHub()
	ctx := context.Background()
	snap := &hooks.SessionSnap{ID: "root", LineRoot: "root", SeedKind: "new", Messages: []types.Message{
		{Role: types.RoleUser, Content: "one"},
		{Role: types.RoleAssistant, Content: "two"},
		{Role: types.RoleUser, Content: "three"},
	}}
	h.LoadBranch("root", snap)
	// 一个已结束分身：tail 模式应跳过它，完整模式才带回摘要
	if err := os.MkdirAll("sessions/root/forks/fork-1", 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(&hooks.SessionSnap{ID: "fork-1", Input: "sub", ForkAnswer: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("sessions/root/forks/fork-1/session.json", data, 0o644); err != nil {
		t.Fatal(err)
	}

	d := (&SessionService{Hub: h}).History(ctx, "root", 2)
	if len(d.Messages) != 2 || d.Start != 1 {
		t.Fatalf("tail history len=%d start=%d, want 2 and 1", len(d.Messages), d.Start)
	}
	if d.Messages[0].Content != "two" || d.Messages[1].Content != "three" {
		t.Fatalf("unexpected tail messages: %+v", d.Messages)
	}
	if len(d.Forks) != 0 {
		t.Fatalf("tail history should skip forks, got %+v", d.Forks)
	}
	full := (&SessionService{Hub: h}).History(ctx, "root", 0)
	if len(full.Messages) != 3 || len(full.Forks) != 1 || full.Forks[0].Answer != "done" {
		t.Fatalf("full history should include fork summary, got msgs=%d forks=%+v", len(full.Messages), full.Forks)
	}
}
