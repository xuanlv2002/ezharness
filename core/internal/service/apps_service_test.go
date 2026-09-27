package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ezharness/core/internal/osfs"
)

/* fakeTerm 记录 Launch 的入参并按需回放会话清单（不起真 PTY）。 */
type fakeTerm struct {
	calls    []launchCall
	sessions []TermInfo
}

type launchCall struct{ Dir, Name, Desc, Origin, Command string }

func (f *fakeTerm) Launch(dir, name, desc, origin, command string) (*TermInfo, error) {
	f.calls = append(f.calls, launchCall{dir, name, desc, origin, command})
	info := TermInfo{ID: "t1", Name: name, Desc: desc, Origin: origin}
	f.sessions = append(f.sessions, info)
	return &info, nil
}

func (f *fakeTerm) List() []TermInfo { return f.sessions }

/* makeApp 造一个应用目录：app.quick 声明 + entry 前端。 */
func makeApp(t *testing.T, name, spec, entry, html string) {
	t.Helper()
	dir := filepath.Join("apps", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.quick"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if entry != "" {
		if err := os.WriteFile(filepath.Join(dir, entry), []byte(html), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

/* 扫描：只有含 app.quick 的目录算应用；标题按 声明 > 入口 <title> > 目录名 回落。 */
func TestAppsList(t *testing.T) {
	t.Chdir(t.TempDir())
	_ = os.MkdirAll("apps", 0o755)

	makeApp(t, "plain", `{"title":"纯前端"}`, "index.html", "<html><title>页面标题</title></html>")
	makeApp(t, "svc", `{"backend":"python probe.py"}`, "index.html", "<html><title>证书探测</title></html>")
	makeApp(t, "named", `{}`, "index.html", "<html>无标题</html>")
	makeApp(t, "nospec", "", "index.html", "<html></html>")             // 无 app.quick：不是应用
	makeApp(t, "broken", `{不是 json`, "index.html", "<html></html>")     // 声明损坏：跳过
	makeApp(t, "escape", `{"entry":"../x.html"}`, "", "")               // 入口越界：跳过
	_ = os.WriteFile("apps/loose.html", []byte("<html></html>"), 0o644) // 平铺文件不是应用

	got := map[string]AppEntry{}
	for _, e := range (&AppsService{Fsys: osfs.OS{}}).List() {
		got[e.Name] = e
	}
	if len(got) != 3 {
		t.Fatalf("apps = %+v", got)
	}
	if e := got["plain"]; e.Kind != KindStatic || e.Title != "纯前端" {
		t.Fatalf("plain: %+v", e)
	}
	if e := got["svc"]; e.Kind != KindService || e.Title != "证书探测" {
		t.Fatalf("svc: %+v", e)
	}
	if e := got["named"]; e.Kind != KindStatic || e.Title != "named" {
		t.Fatalf("named: %+v", e)
	}
}

/* 启动：纯前端只回入口路径；带后端则把命令送进以应用目录为 cwd 的终端。 */
func TestAppsLaunch(t *testing.T) {
	t.Chdir(t.TempDir())
	_ = os.MkdirAll("apps", 0o755)
	makeApp(t, "plain", `{"title":"纯前端"}`, "index.html", "<html></html>")
	makeApp(t, "svc", `{"backend":"python probe.py","entry":"main.html"}`, "main.html", "<html><title>探测</title></html>")

	term := &fakeTerm{}
	svc := &AppsService{Fsys: osfs.OS{}, Term: term}

	v, err := svc.Launch("plain")
	if err != nil || v.Path != "/apps/plain/index.html" || v.TermID != "" || v.Kind != KindStatic {
		t.Fatalf("plain: %+v %v", v, err)
	}
	if len(term.calls) != 0 {
		t.Fatalf("纯前端不该起终端: %+v", term.calls)
	}

	v, err = svc.Launch("svc")
	if err != nil {
		t.Fatal(err)
	}
	if v.Path != "/apps/svc/main.html" || v.Title != "探测" || v.TermID != "t1" {
		t.Fatalf("svc: %+v", v)
	}
	if len(term.calls) != 1 {
		t.Fatalf("calls: %+v", term.calls)
	}
	c := term.calls[0]
	if c.Origin != "快应用·svc" || c.Name != "快应用·svc" || c.Command != "python probe.py" {
		t.Fatalf("call: %+v", c)
	}
	if !filepath.IsAbs(c.Dir) || !strings.HasSuffix(filepath.ToSlash(c.Dir), "/apps/svc") {
		t.Fatalf("cwd 必须是应用目录的绝对路径: %q", c.Dir)
	}

	// 同名后端已在跑：复用会话，不重复起
	if v, err = svc.Launch("svc"); err != nil || v.TermID != "t1" || len(term.calls) != 1 {
		t.Fatalf("reuse: %+v %v calls=%d", v, err, len(term.calls))
	}
	// shell 退出后会话作废，重开一个
	term.sessions[0].Exited = true
	if _, err = svc.Launch("svc"); err != nil || len(term.calls) != 2 {
		t.Fatalf("restart after exit: %v calls=%d", err, len(term.calls))
	}
}

/* 名单外的名字（含穿越写法）一律视作不存在；声明损坏给出具体原因。 */
func TestAppsLaunchErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	_ = os.MkdirAll("apps", 0o755)
	makeApp(t, "broken", `{不是 json`, "index.html", "<html></html>")
	svc := &AppsService{Fsys: osfs.OS{}, Term: &fakeTerm{}}

	for _, name := range []string{"", "nope", "..", "../x", "a/b", `a\b`} {
		if _, err := svc.Launch(name); !errors.Is(err, ErrAppNotFound) {
			t.Fatalf("Launch(%q) = %v; want ErrAppNotFound", name, err)
		}
	}
	if _, err := svc.Launch("broken"); err == nil || errors.Is(err, ErrAppNotFound) {
		t.Fatalf("损坏的声明要给出原因: %v", err)
	}
}

/* 未接线终端服务时，带后端应用明确报错而不是静默只开窗。 */
func TestAppsLaunchWithoutTerminal(t *testing.T) {
	t.Chdir(t.TempDir())
	_ = os.MkdirAll("apps", 0o755)
	makeApp(t, "svc", `{"backend":"python probe.py"}`, "index.html", "<html></html>")
	if _, err := (&AppsService{Fsys: osfs.OS{}}).Launch("svc"); err == nil {
		t.Fatal("Term 为空必须报错")
	}
}
