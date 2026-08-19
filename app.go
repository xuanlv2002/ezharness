/*
app 是应用生命周期管理：持有当前一代 http.Server 与配置快照。

重启 = 换代：关闭旧 server → 切数据目录 → 重建 Hub/Router → 新 server
同端口或预占的新端口上服务。boot 计数由 service 层持有，前端以
health.boot 与 restart 响应的 boot 匹配来判断新服务已就绪。
*/
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"ezharness/internal/config"
	"ezharness/internal/service"
)

type app struct {
	mu   sync.Mutex
	cfg  config.Config
	srv  *http.Server
	boot atomic.Int64 // 服务代际（换代重启递增，跨代共享）
}

/* newApp 创建应用并切到数据目录（进程 cwd 即数据根）。 */
func newApp(c config.Config) (*app, error) {
	adoptLegacy(c.DataDir)
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.Chdir(c.DataDir); err != nil {
		return nil, err
	}
	return &app{cfg: c}, nil
}

/*
adoptLegacy 收编旧版（cwd 直存）数据：数据目录是默认值且尚无
settings.json，而应用根存在旧布局（settings.json / sessions/）时，
整体拷入（已存在的文件跳过）。仅升级首跑发生一次。
*/
func adoptLegacy(dataDir string) {
	if filepath.Clean(dataDir) != filepath.Clean(config.ResolveDataDir("")) {
		return
	}
	if _, err := os.Stat(filepath.Join(dataDir, "settings.json")); err == nil {
		return
	}
	root := config.Root()
	hasOld := false
	for _, f := range []string{"settings.json", "topics.json", "memory.md", "mcp.json", "sessions"} {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			hasOld = true
			break
		}
	}
	if !hasOld {
		return
	}
	_ = service.MigrateData(root, dataDir)
}

/* start 启动第一代 server（端口占用失败即退出）。 */
func (a *app) start() error {
	cfg := a.snapshot()
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: a.buildRouter(cfg)}
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

func (a *app) addr() string { return fmt.Sprintf(":%d", a.cfg.Port) }

/*
restart 换代重启（由 AppService 异步调用，此刻 HTTP 响应已写完，
Shutdown 不会与活跃 handler 死锁）。ln 非 nil 时是预占的新端口 listener。
*/
func (a *app) restart(port int, dataDir string, ln net.Listener) {
	a.mu.Lock()
	old := a.srv
	a.mu.Unlock()
	if old != nil {
		_ = old.Shutdown(context.Background())
	}
	if err := os.MkdirAll(dataDir, 0o755); err == nil {
		_ = os.Chdir(dataDir)
	}
	cfg := a.snapshot()
	cfg.Port, cfg.DataDir = port, dataDir
	srv := &http.Server{Handler: a.buildRouter(cfg)}
	a.mu.Lock()
	a.cfg.Port, a.cfg.DataDir = port, dataDir
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

/* stop 关停当前 server（窗口关闭/进程信号时）。 */
func (a *app) stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.srv != nil {
		_ = a.srv.Shutdown(context.Background())
	}
}
