package builtinskill

import (
	"strings"
	"testing"
)

// 内嵌技能可解析：name/description/instructions 齐全，块标量指示符不泄漏。
func TestSkillsEmbedded(t *testing.T) {
	skills := Skills()
	if len(skills) < 2 {
		t.Fatalf("builtin skills: %+v", skills)
	}
	for _, s := range skills {
		if s.Name == "" || s.Description == "" || s.Instructions == "" || s.Path == "" {
			t.Fatalf("incomplete skill: %+v", s)
		}
		if strings.ContainsAny(s.Description[:1], "|>") {
			t.Fatalf("block scalar indicator leaked: %q", s.Description)
		}
		if !IsBuiltin(strings.TrimSuffix(s.Path, "/SKILL.md")) {
			t.Fatalf("IsBuiltin must accept %q", s.Path)
		}
	}
	if IsBuiltin("no-such-skill") {
		t.Fatal("IsBuiltin must reject unknown dir")
	}
	// 钉住技能名：embed 资源被误删/误 ignore 时立刻暴露
	if !IsBuiltin("quick-app") || !IsBuiltin("callback") {
		t.Fatal("missing builtin skill: quick-app / callback")
	}
}

/* TestMediaSkillEmbedded 校验多媒体技能随应用内嵌（目录名与清单条目是身份，缺失会让 agent 不知道 media 工具/接口的用法）。 */
func TestMediaSkillEmbedded(t *testing.T) {
	for _, s := range Skills() {
		if s.Name == "media" {
			if s.Description == "" {
				t.Fatal("media skill has empty description")
			}
			return
		}
	}
	t.Fatal("builtin media skill missing")
}
