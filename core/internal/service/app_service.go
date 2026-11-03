/*
AppService 是应用级用例：配置查询、格式化与换代重启。

boot 是服务代际计数（原子）：restart/format 先递增并随响应返回，新代
server 的 health 报告同一值——前端轮询 health.boot 与之匹配即可
判断新服务已就绪，再行跳转。RestartFn/FormatFn（main 注入）必须异步
语义：调用即返回，内部换完代后新 server 开始服务。
*/
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"ezharness/core/internal/config"
)

/*
	AppService 管理应用配置与重启。Boot 由 main 持有并注入（跨代共享的

服务代际计数）。
*/
type AppService struct {
	Cfg       func() config.Config
	RestartFn func(port int, listen string, ln net.Listener)
	FormatFn  func(port int, listen string)
	Boot      *atomic.Int64
}

/* AppStatus 是应用配置视图。 */
type AppStatus struct {
	Port    int    `json:"port"`
	Listen  string `json:"listen"`
	DataDir string `json:"dataDir"`
	Boot    int64  `json:"boot"`
}

/* Status 返回当前配置与代际。 */
func (s *AppService) Status() AppStatus {
	c := s.Cfg()
	return AppStatus{Port: c.Port, Listen: c.Listen, DataDir: c.DataDir, Boot: s.Boot.Load()}
}

/* updateFeed 是版本检查数据源（GitHub 最新 release；pre-release/draft 不计入）。 */
const updateFeed = "https://api.github.com/repos/xuanlv2002/ezharness/releases/latest"

/* ReleaseInfo 是最新 release 摘要（是否可升级由前端拿本地版本比对）。 */
type ReleaseInfo struct {
	Latest string `json:"latest"` // 最新版本 tag（v1.0.2 形态）
	URL    string `json:"url"`    // 发布页
}

/*
LatestRelease 查 GitHub 最新 release。8 秒超时：这是设置页手动触发的
检查，网络不可达（离线/被墙）要尽快回话而不是挂住页面；失败归因为
用户可读的一句话（细节不透传，前端只展示这句话）。
*/
func (s *AppService) LatestRelease(ctx context.Context) (ReleaseInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateFeed, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ReleaseInfo{}, errors.New("网络超时，请检查网络后重试")
		}
		return ReleaseInfo{}, errors.New("网络不可达，请检查网络后重试")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("GitHub 返回 %s", res.Status)
	}
	var feed struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&feed); err != nil {
		return ReleaseInfo{}, errors.New("GitHub 响应异常，请稍后重试")
	}
	if feed.TagName == "" {
		return ReleaseInfo{}, fmt.Errorf("尚无发布版本")
	}
	return ReleaseInfo{Latest: feed.TagName, URL: feed.HTMLURL}, nil
}

/* PathEntry 是数据/配置文件路径条目（设置页路径区）。 */
type PathEntry struct {
	Label string `json:"label"`
	File  string `json:"file"`
	Path  string `json:"path"`
}

/* AppConfigView 是设置页完整配置视图：端口、监听、数据目录、路径清单、记忆三路径。 */
type AppConfigView struct {
	Port    int             `json:"port"`
	Listen  string          `json:"listen"`
	DataDir string          `json:"dataDir"`
	Boot    int64           `json:"boot"`
	Paths   []PathEntry     `json:"paths"`
	Memory  MemoryPathsView `json:"memory"`
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
		Listen:  c.Listen,
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
	Port   *int    `json:"port"`
	Listen *string `json:"listen"`
}

/* RestartResult 是重启响应：前端轮询 health.boot === boot 后跳转 url。 */
type RestartResult struct {
	URL  string `json:"url"`
	Boot int64  `json:"boot"`
}

/*
Restart 校验并落盘新配置（监听/端口）、预占新端口，然后异步换代。
仅当请求值非法或新端口被占时返回错误（旧服务不受影响）。数据目录不在
可改面内：挪盘手改 ezharness.json 的 dataDir 后整进程重启。
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
	listen := cur.Listen
	if req.Listen != nil {
		listen = *req.Listen
	}

	var ln net.Listener
	if port != cur.Port || listen != cur.Listen {
		var err error
		if ln, err = net.Listen("tcp", config.ListenAddr(listen, port)); err != nil {
			return RestartResult{}, fmt.Errorf("%s:%d 监听失败: %w", listen, port, err)
		}
	}
	if err := config.Save(config.Config{Port: port, Listen: listen, DataDir: cur.DataDir}); err != nil {
		if ln != nil {
			_ = ln.Close()
		}
		return RestartResult{}, fmt.Errorf("保存配置失败: %w", err)
	}

	next := s.Boot.Add(1)
	go s.RestartFn(port, listen, ln)
	/* listen=0.0.0.0 时本机访问仍走 127.0.0.1，跳转地址不受影响 */
	return RestartResult{URL: fmt.Sprintf("http://127.0.0.1:%d", port), Boot: next}, nil
}

/*
Format 格式化：清空数据目录的全部内容（配置记录、会话、记忆、上传、
快应用、工作区、日志等），恢复首次运行状态。端口与数据目录位置保留
——它们决定当前页面与桌面壳连到哪，重置会让本页立刻失联；结构配置
的其余字段（窗口尺寸）一并回默认。
*/
func (s *AppService) Format() (RestartResult, error) {
	cur := s.Cfg()
	if err := config.Save(config.Config{Port: cur.Port, Listen: cur.Listen, DataDir: cur.DataDir}); err != nil {
		return RestartResult{}, fmt.Errorf("保存配置失败: %w", err)
	}
	next := s.Boot.Add(1)
	go s.FormatFn(cur.Port, cur.Listen)
	return RestartResult{URL: fmt.Sprintf("http://127.0.0.1:%d", cur.Port), Boot: next}, nil
}
