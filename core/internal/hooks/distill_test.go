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

/* parseDistillSections：固定段 + project:<id> 段（slug 规范化）、未知段名丢弃、无段返回空。 */
func TestParseDistillSections(t *testing.T) {
	in := "===user===\n偏好深色主题\n===soul===\n验证前先 go build\n" +
		"===project:Ezharness Core===\n# ezharness-core\n背景…\n===project.md===\n# 项目索引\n" +
		"===hacker===\n不该出现\n===project:===\n空 id 丢弃\n"
	sec := parseDistillSections(in)
	if got := strings.TrimSpace(sec["user"]); got != "偏好深色主题" {
		t.Fatalf("user = %q", got)
	}
	if got := strings.TrimSpace(sec["soul"]); got != "验证前先 go build" {
		t.Fatalf("soul = %q", got)
	}
	if got := strings.TrimSpace(sec["project:ezharness-core"]); !strings.HasPrefix(got, "# ezharness-core") {
		t.Fatalf("project section = %q", got)
	}
	if _, ok := sec["project.md"]; !ok {
		t.Fatal("index section missing")
	}
	if _, ok := sec["hacker"]; ok {
		t.Fatal("unknown section must be dropped")
	}
	if _, ok := sec["project:"]; ok {
		t.Fatal("empty project id must be dropped")
	}
	if len(parseDistillSections("没有任何分段")) != 0 {
		t.Fatal("no sections → empty map")
	}
}

/* distillToMemory：一次调用分段覆盖写——user 带文件头、soul UNCHANGED 跳过、项目文件与索引原样写入。 */
func TestDistillToMemory(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	reply := "===user===\n偏好简洁回复\n===soul===\nUNCHANGED\n" +
		"===project:ezharness===\n# ezharness\n\n记忆树已落地，归档为单次调用。\n" +
		"===project.md===\n# 项目索引\n\n- ezharness — AI harness 桌面应用 | 记忆: memory/longterm/project-ezharness.md"
	if err := distillToMemory(ctx, fakeProvider{reply}, fsys, nil, "C:/work/ezharness"); err != nil {
		t.Fatal(err)
	}
	userMd := string(fsys[LongtermDir+"/user.md"])
	if !strings.HasPrefix(userMd, "# user\n") || !strings.Contains(userMd, "偏好简洁回复") {
		t.Fatalf("user.md wrong: %q", userMd)
	}
	if _, ok := fsys[LongtermDir+"/soul.md"]; ok {
		t.Fatal("UNCHANGED section must be skipped")
	}
	projMd := string(fsys[LongtermDir+"/project-ezharness.md"])
	if !strings.HasPrefix(projMd, "# ezharness\n") {
		t.Fatalf("project file wrong: %q", projMd)
	}
	idx := string(fsys[ProjectMd])
	if !strings.Contains(idx, "- ezharness — AI harness 桌面应用") || !strings.Contains(idx, "project-ezharness.md") {
		t.Fatalf("index wrong: %q", idx)
	}
}

/* projectSlug：小写化、空白转连字符、去非法字符、压缩连续连字符。 */
func TestProjectSlug(t *testing.T) {
	cases := map[string]string{
		"  Ezharness Core ": "ezharness-core",
		"My_Proj v2":       "my-proj-v2",
		"数据/中心":           "数据中心",
		"---a--b---":       "a-b",
		"":                 "",
	}
	for in, want := range cases {
		if got := projectSlug(in); got != want {
			t.Fatalf("projectSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
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
