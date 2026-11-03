/*
app 是应用生命周期管理：持有当前一代 http.Server 与配置快照。

重启 = 换代：关闭旧 server → 重建 Hub/Router → 新 server 同端口或
预占的新端口上服务。boot 计数由 service 层持有，前端以 health.boot
与 restart 响应的 boot 匹配来判断新服务已就绪。
*/
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ezharness/core/internal/config"
	"ezharness/core/internal/controller"
	"ezharness/core/internal/domain"
	"ezharness/core/internal/service"

	"github.com/xuanlv2002/ezloop/ext/hook/mcp"
)

type app struct {
	mu        sync.Mutex
	cfg       config.Config
	tokens    *controller.TokenStore   // 双令牌：会话（进程内）+ openapi（落盘 openapi.json）
	hub       *domain.Hub              // 当前代领域根（换代重建；退出/换代收尾用）
	srv       *http.Server             // 当前代 HTTP 服务
	term      *service.TerminalService // 当前代共享终端（换代重建；收尾杀全部 shell）
	browser   *service.BrowserService  // 共享浏览器桥（端无关，跨代复用；真实浏览器在 desktop 壳）
	mcpRouter *mcp.Router              // 系统级 MCP router（全局唯一：agent hook 与页面/API 共用连接池，跨代复用）
	mcpAuth   *service.McpAuthManager  // MCP OAuth 认证管理器（全局单例：令牌落盘 + loopback 回调，跨代复用）
	callbacks *service.CallbackService // 当前代定时回调调度器（换代重建；停机先停它）
	boot      atomic.Int64             // 服务代际（换代重启递增，跨代共享）
}

/* setTerm 记录当前代共享终端（buildRouter 装配时调用）。 */
func (a *app) setTerm(t *service.TerminalService) {
	a.mu.Lock()
	a.term = t
	a.mu.Unlock()
}

/* setCallbacks 记录当前代定时回调调度器（buildRouter 装配时调用）。 */
func (a *app) setCallbacks(c *service.CallbackService) {
	a.mu.Lock()
	a.callbacks = c
	a.mu.Unlock()
}

/* newApp 创建应用、切到数据目录并把布局初始化完整，再允许开始监听。 */
func newApp(c config.Config, token string) (*app, error) {
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录 %s 失败: %w", c.DataDir, err)
	}
	if err := os.Chdir(c.DataDir); err != nil {
		return nil, fmt.Errorf("切换到数据目录 %s 失败: %w", c.DataDir, err)
	}
	created, err := service.EnsureLayout()
	if err != nil {
		return nil, err
	}
	if len(created) > 0 {
		log.Printf("数据目录初始化：新建 %d 个配置文件（%s）", len(created), strings.Join(created, "、"))
	}
	// openapi 令牌随数据目录（换代切目录时在 restart 重新加载）
	tokens := controller.NewTokenStore(token, service.LoadOrCreateOpenapiToken())
	return &app{cfg: c, tokens: tokens}, nil
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
数据目录换代不存在（不在可改面内）：换目录 = 手改 ezharness.json 后整进程重启。
*/
func (a *app) restart(port int, listen string, ln net.Listener) {
	a.shutdownGeneration()
	engine := a.buildRouter()
	srv := &http.Server{Handler: engine}
	a.mu.Lock()
	a.cfg.Port, a.cfg.Listen = port, listen
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
format 格式化（由 AppService 异步调用）：先收尾当前代——会话落盘、回调
与终端停止，再清空数据目录全部内容、重建默认布局与新代（端口/监听
不变，页面原地重连即首次运行状态）。
日志文件先关再删：Windows 上打开中的文件删不掉，下次写日志会自动重开。
*/
func (a *app) format(port int, listen string) {
	a.shutdownGeneration()
	config.CloseLogWriter()
	if err := wipeDataDir(); err != nil {
		log.Printf("格式化清理未完全成功: %v", err)
	}
	if _, err := service.EnsureLayout(); err != nil {
		log.Printf("格式化后重建数据目录失败: %v", err)
	}
	/* 跨代常驻服务持有旧目录状态（MCP OAuth 探测缓存、浏览器截图目录）：
	   重置它们，让 buildRouter 按新默认布局重建（桌面桥会自动重连） */
	a.mu.Lock()
	oldRouter, oldAuth := a.mcpRouter, a.mcpAuth
	a.mcpRouter, a.mcpAuth, a.browser = nil, nil, nil
	a.mu.Unlock()
	if oldRouter != nil {
		_ = oldRouter.Close()
	}
	if oldAuth != nil {
		oldAuth.Close()
	}
	// 数据目录已清空：OpenAPI 令牌随新 openapi.json 重新生成
	a.tokens.SetOpenapi(service.LoadOrCreateOpenapiToken())
	engine := a.buildRouter()
	srv := &http.Server{Handler: engine}
	a.mu.Lock()
	a.cfg.Port, a.cfg.Listen = port, listen
	a.srv = srv
	a.mu.Unlock()
	ln, err := net.Listen("tcp", a.addr())
	if err != nil {
		log.Printf("格式化后重新监听失败: %v", err)
		return
	}
	go func() { _ = srv.Serve(ln) }()
}

/*
wipeDataDir 删除数据目录（进程 cwd）下的全部条目，目录本身保留（cwd
不能删）。运行中的可执行文件跳过：dataDir 被指到应用根时不能把 core
自己删了。Windows 上残留句柄可能短暂占文件，重试三轮再放弃。
*/
func wipeDataDir() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return err
	}
	exe, _ := os.Executable()
	exe = filepath.Clean(exe)
	/* 结构配置与运行中的可执行文件不删：前者刚由 Format 归一落盘（dataDir
	   被指到应用根时它会躺在数据目录里），后者删了进程下次就起不来 */
	keep := map[string]bool{filepath.Clean(filepath.Join(config.Root(), "ezharness.json")): true}
	if exe != "" {
		keep[exe] = true
	}
	var failed []string
	for _, e := range entries {
		target := filepath.Clean(filepath.Join(cwd, e.Name()))
		/* 目标目录里住着运行中的 core 时整棵跳过（dataDir 指到应用根/父目录） */
		if keep[target] || (exe != "" && strings.HasPrefix(exe, target+string(os.PathSeparator))) {
			continue
		}
		var rmErr error
		for attempt := 0; attempt < 3; attempt++ {
			if rmErr = os.RemoveAll(target); rmErr == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if rmErr != nil {
			failed = append(failed, fmt.Sprintf("%s（%v）", e.Name(), rmErr))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%s", strings.Join(failed, "、"))
	}
	return nil
}

/*
	stop 关停当前代并释放系统级资源（MCP 连接池；托盘退出/关窗/进程信号时），幂等。

换代走 restart，router 不在此路径释放（跨代常驻）。
*/
func (a *app) stop() {
	a.shutdownGeneration()
	a.mu.Lock()
	r := a.mcpRouter
	m := a.mcpAuth
	a.mcpRouter = nil
	a.mcpAuth = nil
	a.mu.Unlock()
	if r != nil {
		_ = r.Close()
	}
	if m != nil {
		m.Close()
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
	hub, srv, term, callbacks := a.hub, a.srv, a.term, a.callbacks
	a.srv = nil
	a.term = nil
	a.callbacks = nil
	a.mu.Unlock()
	// 先停调度再收尾会话：否则停机/换代期间还会触发新轮或写 callbacks.json
	if callbacks != nil {
		callbacks.Stop()
	}
	if term != nil {
		term.Shutdown(3 * time.Second) // 换代重建终端服务:杀全部旧 shell
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
