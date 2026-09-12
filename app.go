/*
app 是应用生命周期管理：持有当前一代 http.Server 与配置快照。

重启 = 换代：关闭旧 server → 切数据目录 → 重建 Hub/Router → 新 server
同端口或预占的新端口上服务。boot 计数由 service 层持有，前端以
health.boot 与 restart 响应的 boot 匹配来判断新服务已就绪。
*/
package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"ezharness/internal/config"
	"ezharness/internal/controller"
	"ezharness/internal/domain"
	"ezharness/internal/service"
)

type app struct {
	mu     sync.Mutex
	cfg    config.Config
	hub    *domain.Hub // 当前代领域根（换代重建；退出/换代收尾用）
	srv    *http.Server
	winCtl *controller.WindowController
	term    *service.TerminalService // 当前代共享终端（换代重建；收尾杀全部 shell）
	browser *service.BrowserService  // 当前代共享浏览器（换代重建；收尾关被控 Chromium）
	boot    atomic.Int64             // 服务代际（换代重启递增，跨代共享）
}

/* setTerm 记录当前代共享终端（buildRouter 装配时调用）。 */
func (a *app) setTerm(t *service.TerminalService) {
	a.mu.Lock()
	a.term = t
	a.mu.Unlock()
}

/* setBrowser 记录当前代共享浏览器（buildRouter 装配时调用）。 */
func (a *app) setBrowser(b *service.BrowserService) {
	a.mu.Lock()
	a.browser = b
	a.mu.Unlock()
}

/* newApp 创建应用并切到数据目录（进程 cwd 即数据根）。 */
func newApp(c config.Config) (*app, error) {
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.Chdir(c.DataDir); err != nil {
		return nil, err
	}
	return &app{cfg: c}, nil
}

/* start 启动第一代 server（端口占用失败即退出）。 */
func (a *app) start() error {
	cfg := a.snapshot()
	ln, err := net.Listen("tcp", config.ListenAddr(cfg.Listen, cfg.Port))
	if err != nil {
		return err
	}
	engine := a.buildRouter()
	srv := &http.Server{Handler: engine}
	a.mu.Lock()
	a.srv = srv
	a.mu.Unlock()
	go func() { _ = srv.Serve(ln) }()
	return nil
}

/* snapshot 返回当前配置副本。 */
func (a *app) snapshot() config.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

func (a *app) addr() string { return config.ListenAddr(a.cfg.Listen, a.cfg.Port) }

/*
restart 换代重启（由 AppService 异步调用，此刻 HTTP 响应已写完，
Shutdown 不会与活跃 handler 死锁）。ln 非 nil 时是预占的新端口 listener。
先收尾旧代（轮落盘到旧目录后，才切数据目录——否则旧轮 OnEnd 会写进新库）。
*/
func (a *app) restart(port int, listen, dataDir string, ln net.Listener) {
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	a.shutdownGeneration()
	if hub != nil {
		syncDrained(dataDir, hub.Active.ID)
	}
	if err := os.MkdirAll(dataDir, 0o755); err == nil {
		_ = os.Chdir(dataDir)
	}
	engine := a.buildRouter()
	srv := &http.Server{Handler: engine}
	a.mu.Lock()
	a.cfg.Port, a.cfg.Listen, a.cfg.DataDir = port, listen, dataDir
	a.srv = srv
	a.mu.Unlock()
	if ln == nil {
		var err error
		if ln, err = net.Listen("tcp", a.addr()); err != nil {
			fmt.Println("重启失败（端口重听）:", err)
			return
		}
	}
	go func() { _ = srv.Serve(ln) }()
}

/* stop 关停当前代（托盘退出/关窗/进程信号时），幂等。 */
func (a *app) stop() { a.shutdownGeneration() }

/*
shutdownGeneration 收尾当前代：先取消运行轮并等待落盘（轮内历史只在
OnEnd 落盘，不等待直接退出会丢整轮），再直接 Close 关 HTTP——SSE 长连
接永远不会 idle，优雅 Shutdown 必然等满超时（退出/换代被拖慢 3 秒的
原因）；换代时 REST 响应已写完、停机时进程将退，SSE 强断无害（前端
自动重连）。幂等。
*/
func (a *app) shutdownGeneration() {
	a.mu.Lock()
	hub, srv, term, browser := a.hub, a.srv, a.term, a.browser
	a.srv = nil
	a.term = nil
	a.browser = nil
	a.mu.Unlock()
	if term != nil {
		term.Shutdown(3 * time.Second) // 换代=换数据目录:杀全部终端 shell
	}
	if browser != nil {
		browser.Shutdown(3 * time.Second) // 被控浏览器整体退出并清理临时 profile
	}
	if hub != nil {
		hub.Active.Shutdown(5 * time.Second)
		// 后台分支的运行轮同样只在 OnEnd 落盘：逐个收尾，不等待直接退出会丢轮
		for _, s := range hub.Sessions() {
			if s != hub.Active {
				s.Shutdown(5 * time.Second)
			}
		}
	}
	if srv != nil {
		_ = srv.Close() // 立即断开全部连接（含 SSE），handler 随连接退出
	}
}

/*
syncDrained 把收尾轮落盘后的最新数据强制同步到新数据目录：
MigrateData（restart 响应前执行）跳过已存在文件，而收尾轮的
session.json/stats/topics 此刻才写到旧目录，不同步会静默丢失。
仅换目录重启需要。
*/
func syncDrained(dataDir, activeID string) {
	cwd, _ := os.Getwd()
	if activeID == "" || filepath.Clean(cwd) == filepath.Clean(dataDir) {
		return
	}
	for _, f := range []string{"stats.json", "topics.json"} {
		if data, err := os.ReadFile(filepath.Join(cwd, f)); err == nil {
			_ = os.WriteFile(filepath.Join(dataDir, f), data, 0o644)
		}
	}
	_ = forceCopyDir(filepath.Join(cwd, "sessions", activeID), filepath.Join(dataDir, "sessions", activeID))
}

/* forceCopyDir 递归拷贝目录，已存在文件覆盖（收尾数据以旧目录为准）。 */
func forceCopyDir(src, dst string) error {
	items, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, it := range items {
		s, d := filepath.Join(src, it.Name()), filepath.Join(dst, it.Name())
		if it.IsDir() {
			if err := forceCopyDir(s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFileOverwrite(s, d); err != nil {
			return err
		}
	}
	return nil
}

func copyFileOverwrite(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst) // 存在即截断覆盖
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
