package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/osfs"
)

func invoke(t *testing.T, tool types.Tool, args string) string {
	t.Helper()
	out, err := tool.Invoke(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s: %v", tool.Name(), err)
	}
	return out
}

func TestBashEcho(t *testing.T) {
	out := invoke(t, bashTool{}, `{"command":"echo hello-ez"}`)
	if strings.TrimSpace(out) != "hello-ez" {
		t.Fatalf("echo output wrong: %q", out)
	}
}

func TestBashNonZeroExitReturnsOutput(t *testing.T) {
	b := bashTool{}
	out, err := b.Invoke(context.Background(), json.RawMessage(`{"command":"echo boom 1>&2; exit 3"}`))
	if err != nil {
		t.Fatalf("non-zero exit must not be tool error: %v", err)
	}
	if !strings.Contains(out, "boom") || !strings.Contains(out, "[exit code 3]") {
		t.Fatalf("output should contain stderr and exit code: %q", out)
	}
}

func TestBashPipeline(t *testing.T) {
	cmd := "printf 'a@b@c' | tr '@' '\\n' | head -1"
	out := invoke(t, bashTool{}, `{"command":"`+cmd+`"}`)
	if strings.TrimSpace(out) != "a" {
		t.Fatalf("pipeline output wrong: %q", out)
	}
}

func TestReadWriteEdit(t *testing.T) {
	all := All(osfs.OS{}, "")
	if len(all) != 4 {
		t.Fatalf("expect 4 tools, got %d", len(all))
	}
	by := map[string]types.Tool{}
	for _, tl := range all {
		by[tl.Name()] = tl
	}

	if _, err := by["write_file"].Invoke(context.Background(),
		json.RawMessage(`{"path":"C:/tmp/ez-test.txt","content":"v1"}`)); err != nil {
		t.Fatal(err)
	}
	if out := invoke(t, by["read_file"], `{"path":"C:/tmp/ez-test.txt"}`); out != "v1" {
		t.Fatalf("read = %q", out)
	}
	out, err := by["edit_file"].Invoke(context.Background(),
		json.RawMessage(`{"path":"C:/tmp/ez-test.txt","old_text":"v1","new_text":"v2"}`))
	if err != nil || !strings.Contains(out, "1") {
		t.Fatalf("edit = %q, %v", out, err)
	}
	if out := invoke(t, by["read_file"], `{"path":"C:/tmp/ez-test.txt"}`); out != "v2" {
		t.Fatalf("after edit = %q", out)
	}
}
