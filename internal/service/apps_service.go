/*
AppsService 快应用用例：扫描 apps/ 下的 html 小工具（agent 经 save_app
生成），描述取文件 <title>。启动由前端直开 /apps/<file>（路由静态服务）。
*/
package service

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ezharness/internal/osfs"
)

/* AppsDir 是快应用目录（工作目录相对）。 */
const AppsDir = "apps"

/* AppEntry 是快应用卡片数据。 */
type AppEntry struct {
	Name  string `json:"name"` // 文件名（含扩展）
	Title string `json:"title"`
	Kind  string `json:"kind"` // html
	Mtime string `json:"mtime"`
}

/* AppsService 快应用扫描。 */
type AppsService struct {
	Fsys osfs.OS
}

var titleRe = regexp.MustCompile(`(?i)<title[^>]*>([^<]*)</title>`)

/* List 扫描 apps/*.html（目录缺失返回空）。 */
func (s *AppsService) List() []AppEntry {
	entries, err := s.Fsys.List(context.Background(), AppsDir)
	if err != nil {
		return nil
	}
	var out []AppEntry
	for _, e := range entries {
		if e.IsDir || !strings.HasSuffix(strings.ToLower(e.Name), ".html") {
			continue
		}
		name := e.Name
		out = append(out, AppEntry{
			Name:  name,
			Title: appTitle(s.Fsys, name),
			Kind:  "html",
			Mtime: appMtime(name),
		})
	}
	return out
}

/* appTitle 读取 html 的 <title>（读失败或无 title 用文件名去扩展）。 */
func appTitle(fsys osfs.OS, file string) string {
	if data, err := fsys.Read(context.Background(), AppsDir+"/"+file); err == nil {
		if m := titleRe.FindSubmatch(data); len(m) > 1 {
			if t := strings.TrimSpace(string(m[1])); t != "" {
				return t
			}
		}
	}
	return strings.TrimSuffix(file, filepath.Ext(file))
}

func appMtime(file string) string {
	if fi, err := os.Stat(filepath.Join(AppsDir, file)); err == nil {
		return fi.ModTime().Format("2006-01-02")
	}
	return time.Now().Format("2006-01-02")
}
