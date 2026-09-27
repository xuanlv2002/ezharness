/*
AppsService 快应用用例：按 app.quick 协议扫描 apps/ 下的目录应用（含该
声明文件的目录才是快应用），并执行启动——开窗由调用方（desktop 壳/web
新标签页）做，声明了 backend 的应用在这里把后端送进内置终端跑起来。
*/
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ezharness/core/internal/osfs"
	"ezharness/core/internal/quickapp"
)

/* AppsDir 是快应用目录（工作目录相对）。 */
const AppsDir = "apps"

/* 应用形态：static 纯前端只开窗，service 声明了 backend 要起后端。 */
const (
	KindStatic  = "static"
	KindService = "service"
)

/* ErrAppNotFound 表示请求的名字不是可用快应用。 */
var ErrAppNotFound = errors.New("app not found")

/* AppEntry 是快应用卡片数据。 */
type AppEntry struct {
	Name  string `json:"name"` // 应用目录名（应用的稳定身份）
	Title string `json:"title"`
	Kind  string `json:"kind"`
	Mtime string `json:"mtime"`
}

/* AppView 是一次启动的结果：前端入口路径 + 后端终端（无后端时为空）。 */
type AppView struct {
	Path   string `json:"path"`
	Title  string `json:"title"`
	Kind   string `json:"kind"`
	TermID string `json:"termId,omitempty"`
}

/*
appTerm 是快应用启动所需的终端能力（TerminalService 实现；测试注入假实现，
不必真起 PTY）。Launch 只写入命令不等输出，后端日志留在那个终端里。
*/
type appTerm interface {
	Launch(dir, name, desc, origin, command string) (*TermInfo, error)
	List() []TermInfo
}

/* AppsService 快应用扫描与启动。 */
type AppsService struct {
	Fsys osfs.OS
	Term appTerm
}

/* appInfo 是一次声明的解析结果：应用名 + 协议声明 + 已校验的入口。 */
type appInfo struct {
	Name  string
	Spec  quickapp.Spec
	Entry string
}

var titleRe = regexp.MustCompile(`(?i)<title[^>]*>([^<]*)</title>`)

/* List 扫描 apps/ 下的快应用（声明缺失、损坏或入口越界的目录跳过）。 */
func (s *AppsService) List() []AppEntry {
	ctx := context.Background()
	var out []AppEntry
	entries, err := s.Fsys.List(ctx, AppsDir)
	if err != nil {
		return nil // 目录缺失即无应用
	}
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		a, aerr := s.load(ctx, e.Name)
		if aerr != nil {
			continue
		}
		out = append(out, AppEntry{
			Name:  a.Name,
			Title: s.title(ctx, a),
			Kind:  kindOf(a.Spec),
			Mtime: appMtime(a.Name + "/" + quickapp.FileName),
		})
	}
	return out
}

/*
Launch 启动快应用：返回前端入口路径，需要后端时把 backend 命令送进一个
以应用目录为 cwd 的终端。同名后端已在跑（同来源且未退出的会话）就复用它，
不重复起——判定自清理，会话关闭或 shell 退出即无匹配。
*/
func (s *AppsService) Launch(name string) (AppView, error) {
	ctx := context.Background()
	a, err := s.load(ctx, name)
	if err != nil {
		return AppView{}, err
	}
	view := AppView{
		Path:  "/apps/" + a.Name + "/" + a.Entry,
		Title: s.title(ctx, a),
		Kind:  kindOf(a.Spec),
	}
	if !a.Spec.HasBackend() {
		return view, nil
	}
	if s.Term == nil {
		return AppView{}, errors.New("终端服务不可用，无法启动后端")
	}
	origin := AppOrigin(a.Name)
	if id := liveTerm(s.Term.List(), origin); id != "" {
		view.TermID = id
		return view, nil
	}
	dir, aerr := filepath.Abs(filepath.FromSlash(AppsDir + "/" + a.Name))
	if aerr != nil {
		dir = AppsDir + "/" + a.Name
	}
	info, lerr := s.Term.Launch(dir, origin, "快应用后端", origin, a.Spec.Backend)
	if lerr != nil {
		return AppView{}, lerr
	}
	view.TermID = info.ID
	return view, nil
}

/* AppOrigin 是快应用后端终端的来源标记（会话身份与侧边栏显示名共用它）。 */
func AppOrigin(name string) string { return "快应用·" + name }

/* liveTerm 在会话清单里找该来源且未退出的终端 id（没有则空串）。 */
func liveTerm(list []TermInfo, origin string) string {
	for _, t := range list {
		if t.Origin == origin && !t.Exited {
			return t.ID
		}
	}
	return ""
}

/*
load 读一个应用的声明并校验：名字必须是单段（不可含分隔符上溯），
app.quick 缺失即不在名单，声明损坏或入口越界给出具体原因。
*/
func (s *AppsService) load(ctx context.Context, name string) (appInfo, error) {
	if !quickapp.ValidName(name) {
		return appInfo{}, ErrAppNotFound
	}
	rel := AppsDir + "/" + name
	data, err := s.Fsys.Read(ctx, rel+"/"+quickapp.FileName)
	if err != nil {
		return appInfo{}, ErrAppNotFound
	}
	spec, err := quickapp.Parse(data)
	if err != nil {
		return appInfo{}, fmt.Errorf("%s/%s 解析失败: %w", rel, quickapp.FileName, err)
	}
	entry, err := quickapp.SafeEntry(spec.EntryOr())
	if err != nil {
		return appInfo{}, err
	}
	return appInfo{Name: name, Spec: spec, Entry: entry}, nil
}

/* kindOf 按是否声明 backend 定形态。 */
func kindOf(spec quickapp.Spec) string {
	if spec.HasBackend() {
		return KindService
	}
	return KindStatic
}

/* title 取卡片标题：声明的 title > 入口文件的 <title> > 目录名。 */
func (s *AppsService) title(ctx context.Context, a appInfo) string {
	if t := strings.TrimSpace(a.Spec.Title); t != "" {
		return t
	}
	if data, err := s.Fsys.Read(ctx, AppsDir+"/"+a.Name+"/"+a.Entry); err == nil {
		if m := titleRe.FindSubmatch(data); len(m) > 1 {
			if t := strings.TrimSpace(string(m[1])); t != "" {
				return t
			}
		}
	}
	return a.Name
}

/* appMtime 是应用声明的修改日期（读不到用今天）。 */
func appMtime(rel string) string {
	if fi, err := os.Stat(filepath.Join(AppsDir, filepath.FromSlash(rel))); err == nil {
		return fi.ModTime().Format("2006-01-02")
	}
	return time.Now().Format("2006-01-02")
}
