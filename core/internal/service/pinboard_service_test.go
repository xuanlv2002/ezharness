package service

import (
	"context"
	"strings"
	"testing"
)

/* TestPinboardImages 图片按内容嗅探落盘，非图片/超限/非法名拒绝。 */
func TestPinboardImages(t *testing.T) {
	t.Chdir(t.TempDir())
	svc := &PinboardService{}
	ctx := context.Background()
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32))

	url, err := svc.SaveImage(ctx, png)
	if err != nil {
		t.Fatalf("SaveImage: %v", err)
	}
	if !strings.HasPrefix(url, "/api/pinboard/images/img-") || !strings.HasSuffix(url, ".png") {
		t.Fatalf("URL 形状不对: %s", url)
	}
	name := strings.TrimPrefix(url, "/api/pinboard/images/")
	data, mime, err := svc.ReadImage(ctx, name)
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	if string(data) != string(png) || mime != "image/png" {
		t.Fatalf("读回不符: %d bytes, %s", len(data), mime)
	}
	if _, _, err := svc.ReadImage(ctx, "../pinboard.json"); err == nil {
		t.Fatal("非法文件名应拒绝")
	}
	if _, err := svc.SaveImage(ctx, []byte("not an image")); err == nil {
		t.Fatal("非图片应拒绝")
	}
	if _, err := svc.SaveImage(ctx, make([]byte, MaxPinboardImageBytes+1)); err == nil {
		t.Fatal("超限应拒绝")
	}
}

/* TestPinboardRoundTrip 首次读取给空板，保存后原样读回，非法形状拒绝。 */
func TestPinboardRoundTrip(t *testing.T) {
	t.Chdir(t.TempDir()) // 数据文件写进程 cwd（与生产数据目录 chdir 语义一致）
	svc := &PinboardService{}
	ctx := context.Background()

	first, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("首次 Get: %v", err)
	}
	if string(first) != defaultPinboard {
		t.Fatalf("首次应返回空板: %s", first)
	}

	payload := []byte(`{"v":1,"z":12,"cards":[{"id":"c1","text":"hi"}],"done":[]}`)
	if err := svc.Save(ctx, payload); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("读回不符: %s", got)
	}

	if err := svc.Save(ctx, []byte(`{`)); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("非法 JSON 应拒绝: %v", err)
	}
	if err := svc.Save(ctx, []byte(`{"cards":[]}`)); err == nil {
		t.Fatal("缺 done 数组应拒绝")
	}
	if err := svc.Save(ctx, []byte(`{"cards":[],"done":[]}`)); err != nil {
		t.Fatalf("合法空板应通过: %v", err)
	}
	if err := svc.Save(ctx, make([]byte, MaxPinboardBytes+1)); err == nil {
		t.Fatal("超限应拒绝")
	}
}
