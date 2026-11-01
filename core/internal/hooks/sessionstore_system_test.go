package hooks

import (
	"context"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/types"
)

/*
落档契约：快照 SystemPrompt 必须是本轮实际发给模型的 system 全文——
含引擎收集的 Guide 附录（SysPrompt 渲染值没有这段），回顾据此看到完整
系统提示词；SystemBase 仍是恢复用的 base（不含附录）。
*/
func TestStoreSnapshotCapturesFullSystem(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	store := NewStore(fsys, "t1")
	store.BindSys(NewSysPrompt("base", ""), "m")

	// 模拟引擎组装后的消息：system 尾部带 Guide 附录
	assembled := "base\n\n<session>\n…\n</session>\n\n# 系统环境\n当前操作系统：darwin\n\n<tool-guide>\nx\n</tool-guide>"
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: assembled},
		{Role: types.RoleUser, Content: "q"},
		{Role: types.RoleAssistant, Content: "a"},
	})
	if err := store.OnEnd(ctx, state); err != nil {
		t.Fatal(err)
	}

	snap, err := LoadSnap(ctx, fsys, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if snap.SystemPrompt != assembled {
		t.Fatalf("SystemPrompt should capture the assembled system with guide appendix:\n%.120s", snap.SystemPrompt)
	}
	if !strings.Contains(snap.SystemPrompt, "<tool-guide>") || !strings.Contains(snap.SystemPrompt, "系统环境") {
		t.Fatal("SystemPrompt must contain the guide appendix")
	}
	if snap.SystemBase != "base" {
		t.Fatalf("SystemBase stays the restore base, got %q", snap.SystemBase)
	}
}

/* system 消息异常缺席时回退 SysPrompt 渲染值，落档不空。 */
func TestStoreSnapshotSystemFallback(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	store := NewStore(fsys, "t1")
	sys := NewSysPrompt("base", "")
	store.BindSys(sys, "m")

	state := newTestState([]types.Message{{Role: types.RoleUser, Content: "q"}})
	if err := store.OnEnd(ctx, state); err != nil {
		t.Fatal(err)
	}
	snap, err := LoadSnap(ctx, fsys, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if snap.SystemPrompt != sys.Prompt() {
		t.Fatalf("fallback should be sys.Prompt(), got %q", snap.SystemPrompt)
	}
}
