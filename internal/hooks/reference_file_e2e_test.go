package hooks

import (
	"context"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/types"
)

/* fakeProv 立即返回固定回复（无工具调用，一轮即完）。 */
type fakeProv struct{}

func (fakeProv) Name() string { return "fake" }
func (fakeProv) Invoke(_ context.Context, _ *types.ModelRequest) (*types.ModelResponse, error) {
	return &types.ModelResponse{Content: "ok"}, nil
}

/* 端到端：真实 Agent + RefFileHook，带引用轮次的历史必须含 <reference_file>
记录且位于用户输入之前（前端 buildBlocks 按"记录→下一个 user 块"挂 chips）。 */
func TestRefFileE2EInjection(t *testing.T) {
	agent := core.NewAgent(fakeProv{}, core.WithHooks(NewRefFile()))
	state, err := agent.Run(context.Background(), "看下这个文件",
		WithRefFiles([]RefFile{{Path: "C:/w/tmp/att-1-0-note.md"}}))
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, m := range state.Messages {
		sb.WriteString(string(m.Role))
		sb.WriteString(":"+m.Content+"\n---\n")
	}
	all := sb.String()
	if !strings.Contains(all, "<reference_file>") {
		t.Fatalf("no reference_file in messages:\n%s", all)
	}
	if !strings.Contains(all, "- C:/w/tmp/att-1-0-note.md") {
		t.Fatalf("ref path missing:\n%s", all)
	}
	// 顺序：记录必须在输入前（挂在下一个 user 块）
	refIdx := strings.Index(all, "<reference_file>")
	inputIdx := strings.Index(all, "看下这个文件")
	if refIdx > inputIdx {
		t.Fatalf("reference_file must precede input:\n%s", all)
	}
	t.Log("\n" + all)
}
