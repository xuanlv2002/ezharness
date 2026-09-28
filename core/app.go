/*
app 是应用生命周期管理：持有当前一代 http.Server 与配置快照。

重启 = 换代：关闭旧 server → 切数据目录 → 重建 Hub/Router → 新 server
同端口或预占的新端口上服务。boot 计数由 service 层持有，前端以
health.boot 与 restart 响应的 boot 匹配来判断新服务已就绪。
*/
package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"ezharness/core/internal/config"
	"ezharness/core/internal/domain"
	"ezharness/core/internal/osfs"
	"ezharness/core/internal/service"

	"github.com/xuanlv2002/ezloop/ext/hook/mcp"
)

type app struct {
	mu        sync.Mutex
	cfg       config.Config
	token     string                   // API 访问令牌（进程级常量，换代沿用）
	hub       *domain.Hub              // 当前代领域根（换代重建；退出/换代收尾用）
	srv       *http.Server             // 当前代 HTTP 服务
	term      *service.TerminalService // 当前代共享终端（换代重建；收尾杀全部 shell）
	browser   *service.BrowserService  // 共享浏览器桥（端无关，跨代复用；真实浏览器在 desktop 壳）
	mcpRouter *mcp.Router              // 系统级 MCP router（全局唯一：agent hook 与页面/API 共用连接池，跨代复用）
	boot      atomic.Int64             // 服务代际（换代重启递增，跨代共享）
}

/* setTerm 记录当前代共享终端（buildRouter 装配时调用）。 */
func (a *app) setTerm(t *service.TerminalService) {
	a.mu.Lock()
	a.term = t
	a.mu.Unlock()
}

/* newApp 创建应用并切到数据目录（进程 cwd 即数据根）。 */
func newApp(c config.Config, token string) (*app, error) {
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.Chdir(c.DataDir); err != nil {
		return nil, err
	}
	return &app{cfg: c, token: token}, nil
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
		syncDrained(dataDir, hub.ActiveSession().ID)
	}
	if err := os.MkdirAll(dataDir, 0o755); err == nil {
		_ = os.Chdir(dataDir)
	}
	config.FollowDataDir(dataDir) // 日志文件跟随数据目录换代
	engine := a.buildRouter()
	srv := &http.Server{Handler: engine}
	a.mu.Lock()
	a.cfg.Port, a.cfg.Listen, a.cfg.DataDir = port, listen, dataDir
	a.srv = srv
	a.mu.Unlock()
	if ln == nil {
		var err error
		if ln, err = net.Listen("tcp", a.addr()); err != nil {
			log.Printf("重启失败（端口重听）: %v", err)
			return
		}
	}
	go func() { _ = srv.Serve(ln) }()
}

/*
	stop 关停当前代并释放系统级资源（MCP 连接池；托盘退出/关窗/进程信号时），幂等。

换代走 restart，router 不在此路径释放（跨代常驻）。
*/
func (a *app) stop() {
	a.shutdownGeneration()
	a.mu.Lock()
	r := a.mcpRouter
	a.mcpRouter = nil
	a.mu.Unlock()
	if r != nil {
		_ = r.Close()
	}
}

/*
shutdownGeneration 收尾当前代：先取消运行轮并等待落盘（轮内历史只在
OnEnd 落盘，不等待直接退出会丢整轮），再直接 Close 关 HTTP——SSE 长连
接永远不会 idle，优雅 Shutdown 必然等满超时（退出/换代被拖慢 3 秒的
原因）；换代时 REST 响应已写完、停机时进程将退，SSE 强断无害（前端
自动重连）。幂等。
*/
func (a *app) shutdownGeneration() {
	a.mu.Lock()
	hub, srv, term := a.hub, a.srv, a.term
	a.srv = nil
	a.term = nil
	a.mu.Unlock()
	if term != nil {
		term.Shutdown(3 * time.Second) // 换代=换数据目录:杀全部终端 shell
	}
	if hub != nil {
		active := hub.ActiveSession()
		if active != nil {
			active.Shutdown(5 * time.Second)
		}
		// 后台分支的运行轮同样只在 OnEnd 落盘：逐个收尾，不等待直接退出会丢轮
		for _, s := range hub.Sessions() {
			if s != active {
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
			if wErr := os.WriteFile(filepath.Join(dataDir, f), data, 0o644); wErr != nil {
				log.Printf("换代同步 %s 失败: %v", f, wErr)
			}
		}
	}
	if err := osfs.CopyDir(filepath.Join(cwd, "sessions", activeID), filepath.Join(dataDir, "sessions", activeID), true); err != nil {
		log.Printf("换代同步活动会话失败（%s）: %v", activeID, err)
	}
}
