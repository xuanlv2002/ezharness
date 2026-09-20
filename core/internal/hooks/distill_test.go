package hooks

import (
	"context"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/types"
)

/* normalize：四节齐原样、缺节补占位、全缺包成【关键事实】、剥代码围栏。 */
func TestNormalizeStructuredSummary(t *testing.T) {
	full := "【已完成】\n- 搭好脚手架\n【正在做】\n- 调通登录\n【待办】\n- 写文档\n【关键事实】\n- 用户要中文"
	if got := normalizeStructuredSummary(full); got != full {
		t.Fatalf("full sections must pass through, got %q", got)
	}
	partial := "【已完成】\n- 搭好脚手架\n【待办】\n- 写文档"
	got := normalizeStructuredSummary(partial)
	if !strings.Contains(got, "【正在做】\n（无）") || !strings.Contains(got, "【关键事实】\n（无）") {
		t.Fatalf("missing sections must be padded, got %q", got)
	}
	bare := normalizeStructuredSummary("用户要中文回复")
	if !strings.HasPrefix(bare, "【关键事实】\n") || !strings.Contains(bare, "用户要中文回复") {
		t.Fatalf("section-less text must be wrapped as key facts, got %q", bare)
	}
	fenced := normalizeStructuredSummary("```markdown\n" + full + "\n```")
	if !strings.Contains(fenced, "【已完成】") || strings.Contains(fenced, "```") {
		t.Fatalf("code fence must be stripped, got %q", fenced)
	}
}

/* parseDistillSections：固定三段解析、未知段名丢弃、无段返回空。 */
func TestParseDistillSections(t *testing.T) {
	in := "===user===\n偏好深色主题\n===projects===\n在做 ezharness\n===lessons===\nWindows 路径要 ToSlash\n===hacker===\n不该出现\n"
	sec := parseDistillSections(in)
	if got := strings.TrimSpace(sec["user"]); got != "偏好深色主题" {
		t.Fatalf("user = %q", got)
	}
	if got := strings.TrimSpace(sec["projects"]); got != "在做 ezharness" {
		t.Fatalf("projects = %q", got)
	}
	if got := strings.TrimSpace(sec["lessons"]); got != "Windows 路径要 ToSlash" {
		t.Fatalf("lessons = %q", got)
	}
	if _, ok := sec["hacker"]; ok {
		t.Fatal("unknown section must be dropped")
	}
	if len(parseDistillSections("没有任何分段")) != 0 {
		t.Fatal("no sections → empty map")
	}
}

/* distillToMemory：正常三段覆盖写（带文件头）；UNCHANGED 段跳过不写。 */
func TestDistillToMemory(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	reply := "===user===\n偏好简洁回复\n===projects===\nUNCHANGED\n===lessons===\nWindows 用 findstr\n"
	if err := distillToMemory(ctx, fakeProvider{reply}, fsys, nil); err != nil {
		t.Fatal(err)
	}
	userMd := string(fsys[LongtermDir+"/user.md"])
	if !strings.HasPrefix(userMd, "# user\n") || !strings.Contains(userMd, "偏好简洁回复") {
		t.Fatalf("user.md wrong: %q", userMd)
	}
	if _, ok := fsys[LongtermDir+"/projects.md"]; ok {
		t.Fatal("UNCHANGED section must be skipped")
	}
	lessonsMd := string(fsys[LongtermDir+"/lessons.md"])
	if !strings.Contains(lessonsMd, "Windows 用 findstr") {
		t.Fatalf("lessons.md wrong: %q", lessonsMd)
	}
}

/* trim 摘要落盘 progress.md：四节齐 + 会话 ID 头；fsys 未装配时静默跳过。 */
func TestTrimWritesProgress(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	tr := NewTrim(fakeProvider{"【关键事实】\n- 用户要中文"}, nil, 100, 1000, fsys, func() string { return "s9" })
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "base"},
		{Role: types.RoleUser, Content: "q1"}, {Role: types.RoleAssistant, Content: "a1"},
		{Role: types.RoleUser, Content: "q2"}, {Role: types.RoleAssistant, Content: "a2"},
		{Role: types.RoleUser, Content: "q3"}, {Role: types.RoleAssistant, Content: "a3"},
	})
	state.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 999}}
	if err := tr.OnLoop(ctx, state); err != nil {
		t.Fatal(err)
	}
	progress := string(fsys[SessionsDir+"/s9/progress.md"])
	if !strings.Contains(progress, "会话 s9") || !strings.Contains(progress, "【关键事实】") {
		t.Fatalf("progress.md wrong: %q", progress)
	}
	if !strings.Contains(progress, SessionsDir+"/s9/session.json") {
		t.Fatalf("progress.md must reference archive path: %q", progress)
	}
	marker := state.Messages[len(state.Messages)-1].Content
	if !strings.Contains(marker, "sessions/s9/progress.md") || !strings.Contains(marker, "【关键事实】") {
		t.Fatalf("marker must reference progress and carry sections: %q", marker)
	}

	// fsys 未装配：不写文件也不报错
	bare := NewTrim(fakeProvider{"【关键事实】\n- x"}, nil, 100, 1000, nil, nil)
	state2 := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "base"},
		{Role: types.RoleUser, Content: "q1"}, {Role: types.RoleAssistant, Content: "a1"},
	})
	state2.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 999}}
	if err := bare.OnLoop(ctx, state2); err != nil {
		t.Fatal(err)
	}
	if len(fsys) != 1 { // 只应有 s9 的 progress.md
		t.Fatalf("bare trim must not write files, fs has %d entries", len(fsys))
	}
}
