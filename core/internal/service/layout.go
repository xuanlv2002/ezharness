/*
EnsureLayout 在服务开始监听前把数据目录的全部配置文件与运行目录一次建好
（只补缺失，不覆盖已有内容）：首次运行后不再依赖运行期懒创建。任何一项建
不出来都立即返回错误并带出路径——只读安装目录之类的问题在启动时就暴露，
而不是等用户发消息时才发现写不进去。
*/
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"ezharness/core/internal/callback"
	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
	"ezharness/core/internal/osfs"
)

/* layoutFile 是缺失时需要写入默认内容的配置文件。 */
type layoutFile struct {
	path  string
	write func(context.Context, osfs.OS) error
}

/*
EnsureLayout 补齐数据目录布局（进程 cwd 即数据目录），返回本次新建的相对
路径。每次启动都跑一遍：首次创建，之后只做缺失修复。
*/
func EnsureLayout() ([]string, error) {
	ctx := context.Background()
	fsys := osfs.OS{}

	// 目录：会话/记忆/技能/快应用/日志 + 工作目录下的草稿与截图区
	workDir := ResolveWorkDir(LoadSettings(ctx, fsys).WorkDir)
	dirs := []string{
		"sessions", hooks.LongtermDir, hooks.SkillsDir, AppsDir, "logs",
		workDir, filepath.Join(workDir, "tmp"), filepath.Join(workDir, "browser"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("初始化目录 %s 失败（目录不可写？）: %w", d, err)
		}
	}

	files := []layoutFile{
		{"models.json", func(ctx context.Context, fsys osfs.OS) error {
			return SaveModelsConfig(ctx, fsys, domain.DefaultModelsConfig())
		}},
		{"settings.json", func(ctx context.Context, fsys osfs.OS) error {
			return SaveSettings(ctx, fsys, domain.DefaultSettings())
		}},
		{"toolRules.json", func(ctx context.Context, fsys osfs.OS) error {
			return SaveToolRules(ctx, fsys, domain.DefaultToolRules())
		}},
		{"stats.json", func(_ context.Context, fsys osfs.OS) error {
			_, err := domain.EnsureStatsFile(fsys)
			return err
		}},
		{hooks.TopicsJson, func(ctx context.Context, fsys osfs.OS) error {
			return fsys.Write(ctx, hooks.TopicsJson, []byte("[]\n"))
		}},
		{callback.FileName, func(ctx context.Context, fsys osfs.OS) error {
			return fsys.Write(ctx, callback.FileName, []byte("[]\n"))
		}},
		{"mcp.json", func(ctx context.Context, fsys osfs.OS) error {
			data, err := json.MarshalIndent(McpFile{Servers: []McpServerFile{}}, "", "  ")
			if err != nil {
				return err
			}
			return fsys.Write(ctx, "mcp.json", data)
		}},
		{hooks.UserJson, func(ctx context.Context, fsys osfs.OS) error {
			return hooks.SaveItems(ctx, fsys, hooks.UserJson, []hooks.MemoryItem{})
		}},
		{hooks.SoulJson, func(ctx context.Context, fsys osfs.OS) error {
			return hooks.SaveItems(ctx, fsys, hooks.SoulJson, []hooks.MemoryItem{})
		}},
		{hooks.ProjectsJson, func(ctx context.Context, fsys osfs.OS) error {
			return hooks.SaveProjects(ctx, fsys, []hooks.ProjectEntry{})
		}},
	}

	created := make([]string, 0, len(files))
	for _, f := range files {
		if data, err := fsys.Read(ctx, f.path); err == nil && len(data) > 0 {
			continue
		}
		if err := f.write(ctx, fsys); err != nil {
			return created, fmt.Errorf("初始化 %s 失败（数据目录不可写？）: %w", f.path, err)
		}
		created = append(created, f.path)
	}
	return created, nil
}
