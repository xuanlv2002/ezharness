package osfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadRoundtrip(t *testing.T) {
	fsys := OS{}
	ctx := context.Background()
	p := filepath.ToSlash(filepath.Join(t.TempDir(), "sub", "a.txt")) // Write 自动建目录
	if err := fsys.Write(ctx, p, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := fsys.Write(ctx, p, []byte("world")); err != nil { // 覆盖写
		t.Fatal(err)
	}
	data, err := fsys.Read(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "world" {
		t.Fatalf("want world, got %q", data)
	}
	if _, err := fsys.Read(ctx, p+"/nope"); err == nil {
		t.Fatal("read missing file must fail")
	}
}

func TestAppend(t *testing.T) {
	fsys := OS{}
	ctx := context.Background()
	p := filepath.ToSlash(filepath.Join(t.TempDir(), "log.jsonl"))
	for _, s := range []string{"a\n", "b\n"} {
		if err := fsys.Append(ctx, p, []byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := fsys.Read(ctx, p)
	if err != nil || string(data) != "a\nb\n" {
		t.Fatalf("append result = %q err=%v", data, err)
	}
}

func TestList(t *testing.T) {
	fsys := OS{}
	ctx := context.Background()
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "d"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("123"), 0o644)
	entries, err := fsys.List(ctx, filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	for _, e := range entries {
		switch e.Name {
		case "d":
			if !e.IsDir {
				t.Fatal("d must be dir")
			}
		case "f.txt":
			if e.IsDir || e.Size != 3 {
				t.Fatalf("f.txt wrong: %+v", e)
			}
		}
	}
}

func TestEdit(t *testing.T) {
	fsys := OS{}
	ctx := context.Background()
	p := filepath.ToSlash(filepath.Join(t.TempDir(), "a.txt"))
	_ = fsys.Write(ctx, p, []byte("x.y x.y"))

	n, err := fsys.Edit(ctx, p, "x.y", "z")
	if err != nil || n != 2 { // 全部命中都替换
		t.Fatalf("want 2 repl, got n=%d err=%v", n, err)
	}
	data, _ := fsys.Read(ctx, p)
	if string(data) != "z z" {
		t.Fatalf("got %q", data)
	}
	if _, err := fsys.Edit(ctx, p, "nope", "q"); err == nil {
		t.Fatal("not found must fail")
	}
	if _, err := fsys.Edit(ctx, p, "", "q"); err == nil {
		t.Fatal("empty old_text must fail")
	}
}

func TestCopyDir(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	_ = os.MkdirAll(filepath.Join(src, "nested"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "a.txt"), []byte("old"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "nested", "b.txt"), []byte("B"), 0o644)

	if err := CopyDir(filepath.Join(src, "missing"), dst, true); err != nil {
		t.Fatalf("missing src must be no-op, got %v", err) // 源不存在是空操作
	}

	_ = os.WriteFile(filepath.Join(dst, "c.txt"), []byte("keep"), 0o644)
	if err := CopyDir(src, dst, false); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		filepath.Join(dst, "a.txt"):           "old",
		filepath.Join(dst, "nested", "b.txt"): "B",
		filepath.Join(dst, "c.txt"):           "keep", // skip 模式不覆盖已存在
	} {
		if data, err := os.ReadFile(path); err != nil || string(data) != want {
			t.Fatalf("%s = %q err=%v, want %q", path, data, err, want)
		}
	}

	_ = os.WriteFile(filepath.Join(src, "a.txt"), []byte("new"), 0o644)
	if err := CopyDir(src, dst, true); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "a.txt")); string(data) != "new" {
		t.Fatalf("overwrite mode must replace, got %q", data)
	}
}
