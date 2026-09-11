package hooks

import (
	"context"
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
	if !strings.Contains(got, "- C:/data/workspace/tmp/att-x.png") {
		t.Fatalf("whole-file path line missing: %q", got)
	}
	if !strings.Contains(got, "（行 12-13，备注：循环有 bug）") {
		t.Fatalf("annotated item line missing: %q", got)
	}
	if !strings.Contains(got, "  def foo():") {
		t.Fatalf("snippet body missing: %q", got)
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
