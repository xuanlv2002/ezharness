package quickapp

import (
	"strings"
	"testing"
)

/* 声明解析：已知字段各就其位，未识别字段不报错（协议前向兼容）。 */
func TestParse(t *testing.T) {
	s, err := Parse([]byte(`{
		"title": "证书探测",
		"entry": "main.html",
		"backend": "python probe.py",
		"icon": "icon.svg",
		"未来的字段": {"nested": true}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "证书探测" || s.Entry != "main.html" ||
		s.Backend != "python probe.py" || s.Icon != "icon.svg" {
		t.Fatalf("fields: %+v", s)
	}
	if !s.HasBackend() {
		t.Fatal("HasBackend must be true")
	}
	if s.EntryOr() != "main.html" {
		t.Fatalf("EntryOr = %q", s.EntryOr())
	}
	if _, err := Parse([]byte(`不是 json`)); err == nil {
		t.Fatal("invalid json must error")
	}
}

/* 缺省：无 entry 回落 index.html，无 backend 即纯前端。 */
func TestSpecDefaults(t *testing.T) {
	s, err := Parse([]byte(`{"title":"纯前端"}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.EntryOr() != DefaultEntry {
		t.Fatalf("EntryOr = %q", s.EntryOr())
	}
	if s.HasBackend() {
		t.Fatal("HasBackend must be false")
	}
	if data, err := s.Marshal(); err != nil || !strings.Contains(string(data), "纯前端") {
		t.Fatalf("marshal: %s %v", data, err)
	}
}

/* SafeEntry：只放行应用目录内的相对路径。 */
func TestSafeEntry(t *testing.T) {
	ok := map[string]string{"": DefaultEntry, "index.html": "index.html",
		"sub/app.html": "sub/app.html", "./a.html": "./a.html"}
	for in, want := range ok {
		got, err := SafeEntry(in)
		if err != nil || got != want {
			t.Fatalf("SafeEntry(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{"..", "../x.html", "sub/../x.html", "/etc/passwd",
		`C:\x.html`, "//host/share"}
	for _, in := range bad {
		if got, err := SafeEntry(in); err == nil {
			t.Fatalf("SafeEntry(%q) must fail, got %q", in, got)
		}
	}
}

/* 应用名是单段目录名：正常短名放行，穿越与保留字符拒绝。 */
func TestValidName(t *testing.T) {
	for _, ok := range []string{"certprobe", "证书探测", "a_b-1"} {
		if !ValidName(ok) {
			t.Fatalf("ValidName(%q) must pass", ok)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "a:b", "a?b", "a*b"} {
		if ValidName(bad) {
			t.Fatalf("ValidName(%q) must fail", bad)
		}
	}
}
