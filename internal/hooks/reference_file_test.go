package hooks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/types"
)

func TestRefFileOnStart(t *testing.T) {
	h := NewRefFile()
	state := &types.LoopState{Metadata: map[string]any{}, Messages: []types.Message{
		{Role: types.RoleUser, Content: "<agent_status>…</agent_status>"},
		{Role: types.RoleUser, Content: "看下我引用的文件"},
	}}
	WithRefFiles([]RefFile{
		{Path: "C:/data/workspace/tmp/att-x.png"},
		{Path: "C:/data/workspace/main.py", Items: []RefItem{
			{Sel: "def foo():\n    return 1", Note: "循环有 bug", From: 12, To: 13},
		}},
	})(state)
	if err := h.OnStart(context.Background(), state); err != nil {
		t.Fatalf("onstart: %v", err)
	}
	if len(state.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(state.Messages))
	}
	got := state.Messages[1].Content // 插在末条 user（本轮 input）之前
	if !strings.HasPrefix(got, "<"+RefTag+">") || !strings.HasSuffix(got, "</"+RefTag+">") {
		t.Fatalf("bad wrap: %q", got)
	}
	// 标签体纯 JSON：hint + refs（items 空=整文件引用，非空=带标注）
	var payload struct {
		Hint string `json:"hint"`
		Refs []struct {
			Path  string `json:"path"`
			Items []struct {
				Sel  string `json:"sel"`
				Note string `json:"note"`
				From int    `json:"from"`
				To   int    `json:"to"`
			} `json:"items"`
		} `json:"refs"`
	}
	body := strings.TrimSuffix(strings.TrimPrefix(got, "<"+RefTag+">"), "</"+RefTag+">")
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &payload); err != nil {
		t.Fatalf("payload not JSON: %v\n%s", err, got)
	}
	if payload.Hint == "" || len(payload.Refs) != 2 {
		t.Fatalf("payload wrong: %+v", payload)
	}
	if payload.Refs[0].Path != "C:/data/workspace/tmp/att-x.png" || len(payload.Refs[0].Items) != 0 {
		t.Fatalf("whole-file ref wrong: %+v", payload.Refs[0])
	}
	if payload.Refs[1].Items[0].Note != "循环有 bug" || payload.Refs[1].Items[0].Sel == "" {
		t.Fatalf("annotated item wrong: %+v", payload.Refs[1].Items[0])
	}
}

func TestRefFileNoRefs(t *testing.T) {
	h := NewRefFile()
	state := &types.LoopState{Metadata: map[string]any{}, Messages: []types.Message{
		{Role: types.RoleUser, Content: "hi"},
	}}
	if err := h.OnStart(context.Background(), state); err != nil {
		t.Fatalf("onstart: %v", err)
	}
	if len(state.Messages) != 1 {
		t.Fatalf("no-ref turn should not inject, got %d", len(state.Messages))
	}
}

func TestRefItemSnippetTruncated(t *testing.T) {
	h := NewRefFile()
	state := &types.LoopState{Metadata: map[string]any{}, Messages: []types.Message{
		{Role: types.RoleUser, Content: "hi"},
	}}
	WithRefFiles([]RefFile{{Path: "a.txt", Items: []RefItem{
		{Sel: strings.Repeat("字", maxRefSel+10), From: 1, To: 1},
	}}})(state)
	if err := h.OnStart(context.Background(), state); err != nil {
		t.Fatalf("onstart: %v", err)
	}
	got := state.Messages[0].Content
	if !strings.Contains(got, "…（已截断）") {
		t.Fatalf("truncation marker missing")
	}
}
