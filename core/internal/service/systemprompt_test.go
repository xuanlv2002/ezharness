package service

import (
	"context"
	"strings"
	"testing"

	"ezharness/core/internal/callback"
	"ezharness/core/internal/domain"
	"ezharness/core/internal/osfs"
)

/*
base 结构契约的可机检部分：段序固定、段间恰好一个空行、无缺席必需段。
分层契约的其余部分（一事实一归属、跨段只引用不复述）靠段函数划分来守，
见 systemprompt.go 头注释。
*/
func TestBuildSystemBaseStructure(t *testing.T) {
	base := buildSystemBase(context.Background(), domain.Settings{},
		osfs.OS{}, domain.ModelsConfig{}, "")

	// 段序固定；<apps> 恒在（空清单也给占位），清单段可缺席时不破坏其余顺序
	wantOrder := []string{
		"你是 ezharness", "<workspace>", "<memory>", "<apps>",
		"<local_api>", "<action>", "<output>",
	}
	last := -1
	for _, mark := range wantOrder {
		i := strings.Index(base, mark)
		if i < 0 {
			t.Errorf("base 缺少段 %q", mark)
			continue
		}
		if i < last {
			t.Errorf("段 %q 顺序错位（应在位置 %d 之后）", mark, last)
		}
		last = i
	}
	if strings.Contains(base, "\n\n\n") {
		t.Error("base 存在连续空行（段分隔应恰好一个空行）")
	}
	// 分层契约：机制文本不回 base——工具动作清单与 API 端点住在工具描述/技能里
	for _, dup := range []string{"browser_action 只有", "/api/open/model/call", "X-EZ-Open-Token"} {
		if strings.Contains(base, dup) {
			t.Errorf("base 出现机制文本 %q（应唯一归属在工具描述或技能）", dup)
		}
	}
}

/* 回调清单按会话标注：只有绑定当前叶的任务打「本会话」。 */
func TestCallbacksSectionKeepsSessionBinding(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	fsys := osfs.OS{}
	st := callback.New(fsys)
	if _, err := st.Create(ctx, callback.Task{
		ID: "cb-1", Title: "每日总结", Kind: callback.KindSession, Enabled: true,
		Schedule: callback.Schedule{Kind: callback.ScheduleInterval, Every: "24h"},
		Session:  &callback.SessionTarget{SessionID: "s1", Mode: callback.ModeSession, Message: "总结"},
	}); err != nil {
		t.Fatal(err)
	}
	got := callbacksSection(ctx, fsys, "s1")
	if !strings.Contains(got, "每日总结") || !strings.Contains(got, "〔本会话〕") {
		t.Fatalf("same session section = %s", got)
	}
	if other := callbacksSection(ctx, fsys, "s2"); strings.Contains(other, "〔本会话〕") {
		t.Fatalf("other session must not mark: %s", other)
	}
}

/* 脚本任务在清单里显示命令，不要求会话。 */
func TestCallbacksSectionRendersScript(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	fsys := osfs.OS{}
	st := callback.New(fsys)
	if _, err := st.Create(ctx, callback.Task{
		ID: "cb-script", Title: "每日同步", Kind: callback.KindScript, Enabled: true,
		Schedule: callback.Schedule{Kind: callback.ScheduleInterval, Every: "24h"},
		Script:   &callback.ScriptTarget{Command: "./sync.sh"},
	}); err != nil {
		t.Fatal(err)
	}
	got := callbacksSection(ctx, fsys, "s1")
	if !strings.Contains(got, "每日同步") || !strings.Contains(got, "脚本 ./sync.sh") {
		t.Fatalf("script section = %s", got)
	}
}
