package hooks

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xuanlv2002/ezloop/types"
)

/* 元数据读取不碰 trace：消息正文只为 LoadSnap 合成，列表只拿计数。 */
func TestLoadSnapMetaSkipsTrace(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	snap := &SessionSnap{
		ID:    "s1",
		Title: "t",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: "a"},
			{Role: types.RoleAssistant, Content: "b"},
		},
	}
	if err := SaveSnap(ctx, fsys, snap); err != nil {
		t.Fatal(err)
	}
	if snap.MsgCount != 2 {
		t.Fatalf("SaveSnap should fill MsgCount from Messages, got %d", snap.MsgCount)
	}
	if err := AppendMessages(ctx, fsys, "s1", "", snap.Messages); err != nil {
		t.Fatal(err)
	}

	meta, err := LoadSnapMeta(ctx, fsys, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "t" || meta.MsgCount != 2 || meta.Messages != nil {
		t.Fatalf("meta should carry state only, got title=%q msgs=%d messages=%d", meta.Title, meta.MsgCount, len(meta.Messages))
	}

	full, err := LoadSnap(ctx, fsys, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Messages) != 2 {
		t.Fatalf("LoadSnap should still decode trace, got %d messages", len(full.Messages))
	}
}

/* 旧快照没有 msgCount 时按行首前缀统计，不解析消息体、不数 span 行。 */
func TestCountMessagesSkipsSpanLines(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	if err := AppendMessages(ctx, fsys, "s1", "", []types.Message{
		{Role: types.RoleUser, Content: "a"},
		{Role: types.RoleAssistant, Content: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := fsys.Append(ctx, tracePath("s1", ""), []byte(`{"kind":"tool","name":"tool.x"}`+"\n")); err != nil {
		t.Fatal(err)
	}
	if got := CountMessages(ctx, fsys, "s1"); got != 2 {
		t.Fatalf("CountMessages = %d, want 2", got)
	}
}

/* fork 摘要优先用落盘回答；旧快照没有时从档案尾部补读，不整档解码。 */
func TestListForksReadsMetaAndTailAnswer(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	writeFork := func(snap *SessionSnap) {
		data, err := json.Marshal(snap)
		if err != nil {
			t.Fatal(err)
		}
		if err := fsys.Write(ctx, "sessions/s1/forks/"+snap.ID+"/session.json", data); err != nil {
			t.Fatal(err)
		}
	}
	writeFork(&SessionSnap{ID: "task-1", Input: "do it", Iterations: 2, ForkAnswer: "stored answer"})
	writeFork(&SessionSnap{ID: "task-2", Input: "do it too", Iterations: 1})
	if err := AppendMessages(ctx, fsys, "s1", "task-2", []types.Message{
		{Role: types.RoleUser, Content: "task"},
		{Role: types.RoleAssistant, Content: "first"},
		{Role: types.RoleAssistant, Content: "final answer"},
	}); err != nil {
		t.Fatal(err)
	}

	got := ListForks(ctx, fsys, "s1")
	if len(got) != 2 {
		t.Fatalf("ListForks returned %d items", len(got))
	}
	if got[0].Task != "do it" || got[0].Answer != "stored answer" || got[0].Iterations != 2 {
		t.Fatalf("unexpected stored summary: %+v", got[0])
	}
	if got[1].Task != "do it too" || got[1].Answer != "final answer" || got[1].Iterations != 1 {
		t.Fatalf("unexpected tail summary: %+v", got[1])
	}
}

/*
旧版空壳 fork 修复：复制前缀只存过内存（session.json 无消息、trace 未建）
时，RepairForkSnap 沿 TargetID→Anchor 回源补齐并落盘；fork-of-fork 链
逐级解析；已有 trace 的 fork 原样返回（不重写）。
*/
func TestRepairForkSnap(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	seed := func(id, target string, anchor int) {
		t.Helper()
		if err := SaveSnap(ctx, fsys, &SessionSnap{
			ID: id, TargetID: target, SeedKind: "fork", LineRoot: id, Anchor: anchor,
		}); err != nil {
			t.Fatal(err)
		}
	}
	src := []types.Message{
		{Role: types.RoleUser, Content: "q1"},
		{Role: types.RoleAssistant, Content: "a1"},
		{Role: types.RoleUser, Content: "q2"},
	}
	if err := SaveSnap(ctx, fsys, &SessionSnap{ID: "src", SeedKind: "new", Messages: src}); err != nil {
		t.Fatal(err)
	}
	if err := AppendMessages(ctx, fsys, "src", "", src); err != nil {
		t.Fatal(err)
	}
	seed("mid", "src", 3)  // 空壳 fork-of-fork：复制 [q1,a1,q2]
	seed("leaf", "mid", 1) // 空壳 fork：再复制 [q1]

	// 修复前：LoadSnap 合成出空历史（用户看到的“重启后变空”）
	broken, err := LoadSnap(ctx, fsys, "leaf")
	if err != nil {
		t.Fatal(err)
	}
	if len(broken.Messages) != 0 {
		t.Fatalf("precondition: broken fork must load empty, got %d", len(broken.Messages))
	}

	fixed := RepairForkSnap(ctx, fsys, broken)
	if len(fixed.Messages) != 1 || fixed.Messages[0].Content != "q1" {
		t.Fatalf("repaired prefix = %+v", fixed.Messages)
	}
	// 已落盘：重启（重读）后历史仍在；再修一次是幂等短路径
	again, err := LoadSnap(ctx, fsys, "leaf")
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Messages) != 1 || again.Messages[0].Content != "q1" {
		t.Fatalf("repaired trace lost on reload: %+v", again.Messages)
	}
	if same := RepairForkSnap(ctx, fsys, again); len(same.Messages) != 1 {
		t.Fatalf("repair must be idempotent: %+v", same.Messages)
	}
	// 链上中间层也被补齐
	mid, err := LoadSnap(ctx, fsys, "mid")
	if err != nil {
		t.Fatal(err)
	}
	if len(mid.Messages) != 3 || mid.Messages[2].Content != "q2" {
		t.Fatalf("mid fork not repaired: %+v", mid.Messages)
	}
}
