/*
AppService 是应用级用例：配置查询、数据目录迁移与换代重启。

boot 是服务代际计数（原子）：restart 先递增并随响应返回，新代
server 的 health 报告同一值——前端轮询 health.boot 与之匹配即可
判断新服务已就绪，再行跳转。RestartFn（main 注入）必须异步语义：
调用即返回，内部换完代后新 server 开始服务。
*/
package service

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"

	"ezharness/internal/config"
)

/* AppService 管理应用配置与重启。Boot 由 main 持有并注入（跨代共享的
服务代际计数）。 */
type AppService struct {
	Cfg       func() config.Config
	RestartFn func(port int, dataDir string, ln net.Listener)
	Boot      *atomic.Int64
}

/* AppStatus 是应用配置视图。 */
type AppStatus struct {
	Port    int    `json:"port"`
	DataDir string `json:"dataDir"`
	Boot    int64  `json:"boot"`
}

/* Status 返回当前配置与代际。 */
func (s *AppService) Status() AppStatus {
	c := s.Cfg()
	return AppStatus{Port: c.Port, DataDir: c.DataDir, Boot: s.Boot.Load()}
}

/* PathEntry 是数据/配置文件路径条目（设置页路径区）。 */
type PathEntry struct {
	Label string `json:"label"`
	File  string `json:"file"`
	Path  string `json:"path"`
}

/* AppConfigView 是设置页完整配置视图：端口、数据目录、路径清单、记忆三路径。 */
type AppConfigView struct {
	Port    int              `json:"port"`
	DataDir string           `json:"dataDir"`
	Boot    int64            `json:"boot"`
	Paths   []PathEntry      `json:"paths"`
	Memory  MemoryPathsView  `json:"memory"`
}

/* MemoryPathsView 是记忆三文件夹绝对路径。 */
type MemoryPathsView struct {
	Longterm string `json:"longterm"`
	Skills   string `json:"skills"`
	Topics   string `json:"topics"`
}

/* ConfigView 组装设置页数据（数据文件相对进程 cwd，即数据目录）。 */
func (s *AppService) ConfigView() AppConfigView {
	c := s.Cfg()
	cwd, _ := os.Getwd()
	join := func(rel string) string { return filepath.Join(cwd, rel) }
	return AppConfigView{
		Port:    c.Port,
		DataDir: c.DataDir,
		Boot:    s.Boot.Load(),
		Paths: []PathEntry{
			{Label: "结构配置", File: "ezharness.json", Path: filepath.Join(config.Root(), "ezharness.json")},
			{Label: "模型配置", File: "models.json", Path: join("models.json")},
			{Label: "行为设置", File: "settings.json", Path: join("settings.json")},
			{Label: "累计统计", File: "stats.json", Path: join("stats.json")},
			{Label: "话题索引", File: "topics.json", Path: join("topics.json")},
			{Label: "MCP", File: "mcp.json", Path: join("mcp.json")},
			{Label: "会话存档", File: "sessions/", Path: join("sessions")},
			{Label: "附件", File: "uploads/", Path: join("uploads")},
			{Label: "快应用", File: "apps/", Path: join("apps")},
		},
		Memory: MemoryPathsView{
			Longterm: join("memory/longterm"),
			Skills:   join("memory/skills"),
			Topics:   join("sessions"),
		},
	}
}

/* RestartRequest 是重启请求（字段为零值表示不改）。 */
type RestartRequest struct {
	Port    *int   `json:"port"`
	DataDir string `json:"dataDir"`
}

/* RestartResult 是重启响应：前端轮询 health.boot === boot 后跳转 url。 */
type RestartResult struct {
	URL  string `json:"url"`
	Boot int64  `json:"boot"`
}

/*
Restart 校验并落盘新配置、迁移数据目录、预占新端口，然后异步换代。
仅当请求值非法或新端口被占时返回错误（旧服务不受影响）。
*/
func (s *AppService) Restart(req RestartRequest) (RestartResult, error) {
	cur := s.Cfg()
	port := cur.Port
	if req.Port != nil {
		if *req.Port < 1 || *req.Port > 65535 {
			return RestartResult{}, fmt.Errorf("端口非法（1-65535）")
		}
		port = *req.Port
	}
	dataDir := cur.DataDir
	if req.DataDir != "" {
		dataDir = config.ResolveDataDir(req.DataDir)
	}

	if dataDir != cur.DataDir {
		if err := MigrateData(cur.DataDir, dataDir); err != nil {
			return RestartResult{}, fmt.Errorf("迁移数据失败: %w", err)
		}
	}
	var ln net.Listener
	if port != cur.Port {
		var err error
		if ln, err = net.Listen("tcp", fmt.Sprintf(":%d", port)); err != nil {
			return RestartResult{}, fmt.Errorf("端口 %d 监听失败: %w", port, err)
		}
	}
	if err := config.Save(config.Config{Port: port, DataDir: dataDir}); err != nil {
		if ln != nil {
			_ = ln.Close()
		}
		return RestartResult{}, fmt.Errorf("保存配置失败: %w", err)
	}

	next := s.Boot.Add(1)
	go s.RestartFn(port, dataDir, ln)
	return RestartResult{URL: fmt.Sprintf("http://127.0.0.1:%d", port), Boot: next}, nil
}

/*
MigrateData 把旧数据目录的全部运行时数据拷入新目录（已存在的文件
跳过——不覆盖新目录中已有的数据）。sessions/ 整目录递归。
*/
func MigrateData(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, f := range []string{"models.json", "settings.json", "stats.json", "topics.json", "memory.md", "mcp.json"} {
		data, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			continue
		}
		target := filepath.Join(dst, f)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return copyDir(filepath.Join(src, "sessions"), filepath.Join(dst, "sessions"))
}

/* copyDir 递归拷贝目录（已存在的文件跳过）。 */
func copyDir(src, dst string) error {
	items, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, it := range items {
		s, d := filepath.Join(src, it.Name()), filepath.Join(dst, it.Name())
		if it.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
			continue
		}
		if _, err := os.Stat(d); err == nil {
			continue
		}
		if err := copyFile(s, d); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
