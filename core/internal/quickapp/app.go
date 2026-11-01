/*
应用的读取与扫描：目录 + app.quick 是唯一入口；标题回落、应用文档与
「Agent 调用」可调用性判定集中在这里，系统提示词、list_apps、load_app
与 AppsService 共用同一份解析结果。
*/
package quickapp

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/fs"
)

/* ErrNotFound 表示目录不存在或不是快应用（无 app.quick / 名字非法）。 */
var ErrNotFound = errors.New("app not found")

/* Info 是一个已解析的快应用（JSON 形状即 list_apps 的结果）。 */
type Info struct {
	Name        string     `json:"name"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Version     string     `json:"version,omitempty"`
	Entry       string     `json:"entry"`
	Backend     string     `json:"backend,omitempty"`
	Icon        string     `json:"icon,omitempty"`
	Document    string     `json:"document,omitempty"` // 文档相对路径（文件存在时才填）
	Callable    bool       `json:"callable"`           // 文档含「Agent 调用」一节
	Callbacks   []Callback `json:"callbacks,omitempty"`
	Spec        Spec       `json:"-"`
}

var (
	titleRe     = regexp.MustCompile(`(?is)<title[^>]*>([^<]*)</title>`)
	agentCallRe = regexp.MustCompile(`(?im)^#{1,6}[^\n]*agent\s*(调用|call)`)
)

/* Load 解析单个应用；名字非法/目录不是快应用返回 ErrNotFound。 */
func Load(ctx context.Context, fsys fs.FileSystem, name string) (Info, error) {
	if !ValidName(name) {
		return Info{}, ErrNotFound
	}
	rel := AppsDir + "/" + name
	data, err := fsys.Read(ctx, rel+"/"+FileName)
	if err != nil {
		return Info{}, ErrNotFound
	}
	spec, err := Parse(data)
	if err != nil {
		return Info{}, err
	}
	for i := range spec.Callbacks {
		spec.Callbacks[i].Normalize()
	}
	entry, err := SafeEntry(spec.EntryOr())
	if err != nil {
		return Info{}, err
	}
	info := Info{
		Name:        name,
		Title:       titleOf(ctx, fsys, rel, spec, entry, name),
		Description: strings.TrimSpace(spec.Description),
		Version:     strings.TrimSpace(spec.Version),
		Entry:       entry,
		Backend:     strings.TrimSpace(spec.Backend),
		Icon:        strings.TrimSpace(spec.Icon),
		Callbacks:   spec.Callbacks,
		Spec:        spec,
	}
	if document, callable := loadDocument(ctx, fsys, rel, spec); document != "" {
		info.Document = document
		info.Callable = callable
	}
	return info, nil
}

/* Scan 扫描 apps/ 下全部应用（声明损坏或入口越界的目录跳过），按名字排序。 */
func Scan(ctx context.Context, fsys fs.FileSystem) []Info {
	entries, err := fsys.List(ctx, AppsDir)
	if err != nil {
		return nil
	}
	out := make([]Info, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		if info, err := Load(ctx, fsys, entry.Name); err == nil {
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

/* titleOf 取标题：声明 > 入口文件 <title> > 目录名。 */
func titleOf(ctx context.Context, fsys fs.FileSystem, rel string, spec Spec, entry, name string) string {
	if t := strings.TrimSpace(spec.Title); t != "" {
		return t
	}
	if data, err := fsys.Read(ctx, rel+"/"+entry); err == nil {
		if m := titleRe.FindSubmatch(data); len(m) > 1 {
			if t := strings.TrimSpace(string(m[1])); t != "" {
				return t
			}
		}
	}
	return name
}

/*
loadDocument 读取应用文档（缺省 APP.md），返回相对路径与是否对 agent 开放。
文档缺失/路径越界视为没有调用面；是否可调用只看有没有「Agent 调用」标题。
*/
func loadDocument(ctx context.Context, fsys fs.FileSystem, rel string, spec Spec) (string, bool) {
	path, err := SafeEntry(spec.DocumentOr())
	if err != nil {
		return "", false
	}
	data, err := fsys.Read(ctx, rel+"/"+path)
	if err != nil {
		return "", false
	}
	return path, agentCallRe.Match(data)
}
