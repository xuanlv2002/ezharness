package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/osfs"
	"ezharness/core/internal/service"
)

/*
启动接口的入参校验与名单映射（纯前端应用即可覆盖到 200——带后端应用
要真起终端，其终端复用与 cwd 归 service 的 appTerm 假实现覆盖）。
*/
func TestAppOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Chdir(t.TempDir())
	dir := filepath.Join("apps", "certprobe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.quick"), []byte(`{"title":"证书探测"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &AppsController{Svc: &service.AppsService{Fsys: osfs.OS{}}}
	post := func(body string) (int, string) {
		w := httptest.NewRecorder()
		g, _ := gin.CreateTestContext(w)
		g.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		g.Request.Header.Set("Content-Type", "application/json")
		c.Open(g)
		return w.Code, w.Body.String()
	}

	if got, _ := post(`{}`); got != http.StatusBadRequest {
		t.Fatalf("empty body: %d", got)
	}
	for _, body := range []string{`{"name":"nope"}`, `{"name":".."}`, `{"name":"../x"}`, `{"name":"a/b"}`} {
		if got, _ := post(body); got != http.StatusNotFound {
			t.Fatalf("%s: %d", body, got)
		}
	}
	got, body := post(`{"name":"certprobe"}`)
	if got != http.StatusOK {
		t.Fatalf("open: %d %s", got, body)
	}
	for _, want := range []string{`"path":"/apps/certprobe/index.html"`, `"title":"证书探测 · ezharness"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("open body missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, `"termId"`) {
		t.Fatalf("纯前端应用不该有 termId: %s", body)
	}
}
