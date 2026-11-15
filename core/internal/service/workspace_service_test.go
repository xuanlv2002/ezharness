package service

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

/* 附件卡元数据：大小恒有；pptx 从 zip 中央目录数幻灯片；其余格式不报页数。 */
func TestWorkspaceMeta(t *testing.T) {
	dir := t.TempDir()

	ppt := filepath.Join(dir, "deck.pptx")
	f, err := os.Create(ppt)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{
		"ppt/slides/slide1.xml",
		"ppt/slides/slide2.xml",
		"ppt/slides/slide10.xml",
		"ppt/slides/_rels/slide1.xml.rels",  // 关系文件不算一页
		"ppt/slideLayouts/slideLayout1.xml", // 版式不算一页
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("<x/>")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	svc := &WorkspaceService{}
	m, err := svc.Meta(ppt)
	if err != nil {
		t.Fatal(err)
	}
	if m.Size <= 0 {
		t.Fatalf("size = %d", m.Size)
	}
	if m.Pages != 3 {
		t.Fatalf("pages = %d, want 3", m.Pages)
	}

	txt := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(txt, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err = svc.Meta(txt)
	if err != nil || m.Size != 5 || m.Pages != 0 {
		t.Fatalf("txt meta = %+v err=%v", m, err)
	}

	if _, err := svc.Meta(dir); err == nil {
		t.Fatal("目录应报错")
	}
	if _, err := svc.Meta(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("不存在的文件应报错")
	}
}
