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
}
