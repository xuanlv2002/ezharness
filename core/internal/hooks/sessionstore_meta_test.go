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
