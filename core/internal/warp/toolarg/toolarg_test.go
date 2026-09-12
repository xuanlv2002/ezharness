package toolarg

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/types"
)

type fakeFS struct{ files map[string][]byte }

func (f fakeFS) Read(_ context.Context, p string) ([]byte, error) {
	if d, ok := f.files[p]; ok {
		return d, nil
	}
	return nil, errMissing{}
}
func (f fakeFS) Write(_ context.Context, _ string, _ []byte) error { return nil }
func (f fakeFS) List(_ context.Context, _ string) ([]fs.Entry, error) {
	return nil, nil
}
func (f fakeFS) Edit(_ context.Context, _, _, _ string) (int, error) { return 0, nil }

type errMissing struct{}

func (errMissing) Error() string { return "missing" }

/* 记录参数的工具桩 */
type stubTool struct {
	gotArgs json.RawMessage
}

func (s *stubTool) Name() string                { return "stub" }
func (s *stubTool) Description() string         { return "" }
func (s *stubTool) ArgsSchema() json.RawMessage { return nil }
func (s *stubTool) Invoke(_ context.Context, args json.RawMessage) (string, error) {
	s.gotArgs = args
	return "ok", nil
}

func wrap(files map[string][]byte) (*stubTool, types.Tool) {
	stub := &stubTool{}
	return stub, Warp(fakeFS{files: files})(nil, stub)
}

func TestExpandNestedAndEscape(t *testing.T) {
	stub, tool := wrap(map[string][]byte{
		"/tmp/a.txt": []byte("line1\n\"quoted\" & <tag>"),
	})
	raw := json.RawMessage(`{"path":"/out.md","content":"prefix <@toolArg>/tmp/a.txt</@toolArg> suffix","opts":{"deep":["<@toolArg>/tmp/a.txt</@toolArg>"]}}`)
	if _, err := tool.Invoke(context.Background(), raw); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	var got struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Opts    struct {
			Deep []string `json:"deep"`
		} `json:"opts"`
	}
	if err := json.Unmarshal(stub.gotArgs, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := `prefix line1\n"quoted" & <tag> suffix`
	want = strings.ReplaceAll(want, `\n`, "\n")
	if got.Content != want {
		t.Fatalf("content = %q, want %q", got.Content, want)
	}
	if len(got.Opts.Deep) != 1 || got.Opts.Deep[0] != "line1\n\"quoted\" & <tag>" {
		t.Fatalf("deep = %#v", got.Opts.Deep)
	}
}

func TestMissingRefErrors(t *testing.T) {
	_, tool := wrap(map[string][]byte{})
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{"content":"<@toolArg>/nope.txt</@toolArg>"}`))
	if err == nil || !strings.Contains(err.Error(), "/nope.txt") {
		t.Fatalf("err = %v, want missing-ref error", err)
	}
}

func TestNoTagPassthrough(t *testing.T) {
	stub, tool := wrap(map[string][]byte{})
	raw := json.RawMessage(`{"content":"plain <@toolArg"}`)
	if _, err := tool.Invoke(context.Background(), raw); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if string(stub.gotArgs) != string(raw) {
		t.Fatalf("args mutated without tags: %s", stub.gotArgs)
	}
}

func TestEmptyRefKeptVerbatim(t *testing.T) {
	stub, tool := wrap(map[string][]byte{})
	raw := json.RawMessage(`{"content":"x <@toolArg></@toolArg> y"}`)
	if _, err := tool.Invoke(context.Background(), raw); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	var got struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(stub.gotArgs, &got)
	if got.Content != "x <@toolArg></@toolArg> y" {
		t.Fatalf("content = %q", got.Content)
	}
}
