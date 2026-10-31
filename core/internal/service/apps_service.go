/*
AppsService 快应用用例：按 app.quick 协议扫描 apps/ 下的目录应用（含该
声明文件的目录才是快应用），并执行启动——开窗由调用方（desktop 壳/web
新标签页）做，声明了 backend 的应用在这里把后端送进内置终端跑起来。
卸载也在这里：停后端终端、删应用目录、清钉选残留。
*/
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/osfs"
	"ezharness/core/internal/quickapp"
)

/* AppsDir 是快应用目录（工作目录相对）。 */
const AppsDir = quickapp.AppsDir

/* PinMax 是桌面小助手钉选上限（钉满后再钉顶掉最早）。 */
const PinMax = 3

/* 应用形态：static 纯前端只开窗，service 声明了 backend 要起后端。 */
const (
	KindStatic  = "static"
	KindService = "service"
)

/* ErrAppNotFound 表示请求的名字不是可用快应用。 */
var ErrAppNotFound = errors.New("app not found")

/* AppEntry 是快应用卡片数据。 */
type AppEntry struct {
	Name        string `json:"name"` // 应用目录名（应用的稳定身份）
	Title       string `json:"title"`
	Description string `json:"description,omitempty"` // 一句话描述
	Version     string `json:"version,omitempty"`     // 语义化版本
	Kind        string `json:"kind"`
	Mtime       string `json:"mtime"`
	Icon        string `json:"icon,omitempty"` // 卡片头像（同源 URL /apps/<名>/<icon>，空=未声明或越界）
	Pinned      bool   `json:"pinned"`         // 已钉到桌面小助手
	Callable    bool   `json:"callable"`       // 应用文档含「Agent 调用」一节
}

/* AppView 是一次启动的结果：前端入口路径 + 后端终端（无后端时为空）。 */
type AppView struct {
	Path   string `json:"path"`
	Title  string `json:"title"`
	Kind   string `json:"kind"`
	TermID string `json:"termId,omitempty"`
}

/*
appTerm 是快应用所需的终端能力（TerminalService 实现；测试注入假实现，
不必真起 PTY）。Launch 只写入命令不等输出，后端日志留在那个终端里；
Close 供卸载收掉仍在跑的后端。
*/
type appTerm interface {
	Launch(dir, name, desc, origin, command string) (*TermInfo, error)
	List() []TermInfo
	Close(id string)
}

/* AppsService 快应用扫描与启动。 */
type AppsService struct {
	Fsys osfs.OS
	Term appTerm
	Hub  *domain.Hub // 钉选列表存全局设置（nil=测试装配，pin 恒 false）
}

/* List 扫描 apps/ 下的快应用（声明缺失、损坏或入口越界的目录跳过）。 */
func (s *AppsService) List(ctx context.Context) []AppEntry {
	apps := quickapp.Scan(ctx, s.Fsys)
	out := make([]AppEntry, 0, len(apps))
	for _, a := range apps {
		out = append(out, AppEntry{
			Name:        a.Name,
			Title:       a.Title,
			Description: a.Description,
			Version:     a.Version,
			Kind:        kindOf(a.Spec),
			Mtime:       appMtime(a.Name + "/" + quickapp.FileName),
			Icon:        iconURL(a.Name, a.Spec.Icon),
			Pinned:      s.Hub != nil && slices.Contains(s.Hub.SettingsSnapshot().PinnedApps, a.Name),
			Callable:    a.Callable,
		})
	}
	return out
}

/*
renderAppsSection 组装 system 的 <apps> 段：应用清单与使用纪律。
每 session 固定；轮内新建/更新/删除后由 list_apps 刷新。
*/
func renderAppsSection(ctx context.Context, fsys osfs.OS) string {
	apps := quickapp.Scan(ctx, fsys)
	var b strings.Builder
	b.WriteString("\n\n<apps>\n（快应用清单，以此为准，不要读取 apps 目录或 app.quick 来发现应用；应用由 save_app 构建。" +
		"使用某个应用前先 load_app(name) 读它的调用文档：只有文档含「Agent 调用」一节（清单标〔可调用〕）才可按文档调用，否则仅面向用户，不要臆造接口。" +
		"本段随会话固定，轮内新增/更新/移除见 <resource_change>；已有应用能满足就先复用，不要重复造功能相同的应用）")
	if len(apps) == 0 {
		b.WriteString("\n（暂无快应用）")
	} else {
		for _, a := range apps {
			b.WriteString("\n- " + a.Name)
			if a.Title != a.Name {
				b.WriteString("（" + a.Title + "）")
			}
			if a.Version != "" {
				b.WriteString(" v" + a.Version)
			}
			if a.Callable {
				b.WriteString("〔可调用〕")
			}
			desc := a.Description
			if desc == "" {
				desc = "（无描述）"
			}
			b.WriteString("：" + desc)
		}
	}
	b.WriteString("\n</apps>")
	return b.String()
}

/*
TogglePin 把快应用钉到桌面小助手（再点一次取消）：翻转全局设置里的
pinnedApps 并落盘。钉满 PinMax 后再钉新的会把最早钉的顶掉（小助手
只展示 3 个钉选）。不重建 agent（钉选不影响任何 agent 行为）。
不存在的应用报 ErrAppNotFound。
*/
func (s *AppsService) TogglePin(ctx context.Context, name string) (bool, error) {
	if _, err := s.load(ctx, name); err != nil {
		return false, err
	}
	if s.Hub == nil {
		return false, errors.New("settings unavailable")
	}
	st := s.Hub.SettingsSnapshot()
	if i := slices.Index(st.PinnedApps, name); i >= 0 {
		st.PinnedApps = slices.Delete(st.PinnedApps, i, i+1)
	} else {
		st.PinnedApps = append(st.PinnedApps, name)
		for len(st.PinnedApps) > PinMax {
			st.PinnedApps = st.PinnedApps[1:]
		}
	}
	if err := SaveSettings(ctx, s.Hub.Fsys, st); err != nil {
		return false, err
	}
	s.Hub.ApplySettings(st)
	return slices.Contains(st.PinnedApps, name), nil
}

/*
Uninstall 卸载快应用：先收掉该应用仍在跑的后端终端（文件被后端占用时
目录删不掉），再删除 apps/<名>/ 整个目录，最后清掉钉选列表里的残留。
名单校验同 Launch（名字不可穿越）；不存在的应用报 ErrAppNotFound。
*/
func (s *AppsService) Uninstall(ctx context.Context, name string) error {
	if _, err := s.load(ctx, name); err != nil {
		return err
	}
	if s.Term != nil {
		if id := liveTerm(s.Term.List(), AppOrigin(name)); id != "" {
			s.Term.Close(id)
		}
	}
	if err := os.RemoveAll(filepath.Join(AppsDir, name)); err != nil {
		return fmt.Errorf("删除应用目录失败: %w", err)
	}
	if s.Hub == nil {
		return nil
	}
	st := s.Hub.SettingsSnapshot()
	if !slices.Contains(st.PinnedApps, name) {
		return nil
	}
	st.PinnedApps = slices.DeleteFunc(st.PinnedApps, func(p string) bool { return p == name })
	if err := SaveSettings(ctx, s.Hub.Fsys, st); err != nil {
		return err
	}
	s.Hub.ApplySettings(st)
	return nil
}

/* iconURL 校验声明的 icon（同 entry 的相对路径约束）并拼成同源 URL；空/越界返回空串。 */
func iconURL(name, icon string) string {
	if strings.TrimSpace(icon) == "" {
		return ""
	}
	p, err := quickapp.SafeEntry(icon)
	if err != nil {
		return ""
	}
	return "/apps/" + name + "/" + p
}

/*
Launch 启动快应用：返回前端入口路径，需要后端时把 backend 命令送进一个
以应用目录为 cwd 的终端。同名后端已在跑（同来源且未退出的会话）就复用它，
不重复起——判定自清理，会话关闭或 shell 退出即无匹配。
*/
func (s *AppsService) Launch(ctx context.Context, name string) (AppView, error) {
	a, err := s.load(ctx, name)
	if err != nil {
		return AppView{}, err
	}
	view := AppView{
		Path:  "/apps/" + a.Name + "/" + a.Entry,
		Title: a.Title,
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
func (s *AppsService) load(ctx context.Context, name string) (quickapp.Info, error) {
	info, err := quickapp.Load(ctx, s.Fsys, name)
	if errors.Is(err, quickapp.ErrNotFound) {
		return quickapp.Info{}, ErrAppNotFound
	}
	if err != nil {
		return quickapp.Info{}, fmt.Errorf("%s/%s 解析失败: %w", AppsDir+"/"+name, quickapp.FileName, err)
	}
	return info, nil
}

/* kindOf 按是否声明 backend 定形态。 */
func kindOf(spec quickapp.Spec) string {
	if spec.HasBackend() {
		return KindService
	}
	return KindStatic
}

/* appMtime 是应用声明的修改日期（读不到用今天）。 */
func appMtime(rel string) string {
	if fi, err := os.Stat(filepath.Join(AppsDir, filepath.FromSlash(rel))); err == nil {
		return fi.ModTime().Format("2006-01-02")
	}
	return time.Now().Format("2006-01-02")
}
