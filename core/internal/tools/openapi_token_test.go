package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCreateOpenapiTokenTool(t *testing.T) {
	tool := OpenapiToken(func(name string) string { return "tok-" + name })
	out, err := tool.Invoke(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "「agent」") || !strings.Contains(out, "tok-agent") {
		t.Fatalf("default token output = %q", out)
	}
	out, err = tool.Invoke(context.Background(), json.RawMessage(`{"name":"quickapp"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "「quickapp」") || !strings.Contains(out, "tok-quickapp") {
		t.Fatalf("named token output = %q", out)
	}
}
