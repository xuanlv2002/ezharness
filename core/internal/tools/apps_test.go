package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ezharness/core/internal/osfs"
	"ezharness/core/internal/quickapp"
)

// save_app 增量语义：未传字段保持原值、html 缺省不动入口、空值清除。
func TestSaveAppIncremental(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx := context.Background()
	fsys := osfs.OS{}
	tool := saveAppTool{fsys: fsys}
	run := func(args string) (string, error) {
		return tool.Invoke(ctx, json.RawMessage(args))
	}
	load := func() quickapp.Info {
		t.Helper()
		info, err := quickapp.Load(ctx, fsys, "demo")
		if err != nil {
			t.Fatalf("load app: %v", err)
		}
		return info
	}

	if _, err := run(`{"name":"demo"}`); err == nil {
		t.Fatal("new app without html must fail")
	}
	if _, err := run(`{"name":"demo","html":"  "}`); err == nil {
		t.Fatal("new app with blank html must fail")
	}
	out, err := run(`{"name":"demo","title":"演示","html":"<h1>v1</h1>","backend":"python app.py",
		"callbacks":[{"name":"daily","url":"http://127.0.0.1:9999/api"}]}`)
	if err != nil || !strings.Contains(out, "已创建") {
		t.Fatalf("create: %q %v", out, err)
	}
	if info := load(); info.Title != "演示" || info.Backend != "python app.py" || len(info.Callbacks) != 1 {
		t.Fatalf("created spec: %+v", info)
	}

	// 只改 icon：其余清单字段与入口文件保持原样。
	before, err := fsys.Read(ctx, "apps/demo/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if out, err = run(`{"name":"demo","icon":"icon.svg"}`); err != nil || !strings.Contains(out, "已更新") {
		t.Fatalf("update icon: %q %v", out, err)
	}
	info := load()
	if info.Icon != "icon.svg" || info.Title != "演示" || info.Backend != "python app.py" || len(info.Callbacks) != 1 {
		t.Fatalf("omitted fields must keep: %+v", info)
	}
	after, err := fsys.Read(ctx, "apps/demo/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("html must stay untouched, before=%q after=%q", before, after)
	}

	// 空串/空数组显式清除。
	if _, err := run(`{"name":"demo","backend":"","callbacks":[]}`); err != nil {
		t.Fatal(err)
	}
	if info := load(); info.Backend != "" || len(info.Callbacks) != 0 {
		t.Fatalf("empty values must clear: %+v", info)
	}
}

// load_app 不再回传 CHANGELOG（要历史直接 read_file）。
func TestLoadAppSkipsChangelog(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx := context.Background()
	fsys := osfs.OS{}
	run := func(args string) (string, error) {
		return saveAppTool{fsys: fsys}.Invoke(ctx, json.RawMessage(args))
	}
	if _, err := run(`{"name":"demo","html":"<h1>v1</h1>","document":"APP.md"}`); err != nil {
		t.Fatal(err)
	}
	if err := fsys.Write(ctx, "apps/demo/APP.md", []byte("# 能力\n演示应用")); err != nil {
		t.Fatal(err)
	}
	if err := fsys.Write(ctx, "apps/demo/CHANGELOG.md", []byte("# 版本历史\nv1.0.0")); err != nil {
		t.Fatal(err)
	}
	out, err := LoadApp(fsys).Invoke(ctx, json.RawMessage(`{"name":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "演示应用") || strings.Contains(out, "版本历史") {
		t.Fatalf("load_app output: %q", out)
	}
}
