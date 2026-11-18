/*
ezharness core：Go sidecar，基于 ezloop 内核的 harness 后端（gin + 三层 MVC）——
启动即对本机全权（文件不限目录 + shell），在哪启动就操作哪台设备。main 只做装配。
*/
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/config"
	"ezharness/core/internal/controller"
	"ezharness/core/internal/domain"
	"ezharness/core/internal/service"
	"ezharness/core/internal/warp/modeldump"
)

/* appStart 进程启动时刻（启动分段计时日志用）。 */
var appStart = time.Now()

func main() {
	root := flag.String("root", "", "应用根目录(配置与数据所在;缺省=exe 所在目录)")
	flag.Parse()
	config.SetRoot(*root)

	c, err := config.Load()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	gin.SetMode(gin.ReleaseMode)

	// 日志落盘（须在 newApp/buildRouter 之前，gin.Logger 构造时捕获 writer）：
	// log + gin 输出写 <数据目录>/logs/，dev 终端同时可见
	logWriter := config.OpenLogWriter(c.DataDir)
	log.SetOutput(logWriter)
	gin.DefaultWriter = logWriter
	gin.DefaultErrorWriter = logWriter
	// modeldump 输出恒进日志（写不写由开关决定，开关随设置/换代在 buildRouter 推导）
	modeldump.Out = logWriter

	a, err := newApp(c)
	if err != nil {
		// 初始化失败常常正是日志目录不可写：stderr 必须直接可见
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		log.Fatalf("启动失败: %v", err)
	}
	if err := a.start(); err != nil {
		/* bind 抢不上即仲裁失败（谁 bind 成功谁是主 core）：查实例簿记给一句指路 */
		if b, rerr := os.ReadFile(filepath.Join(config.Root(), "server.json")); rerr == nil {
			var si struct {
				Port int `json:"port"`
				Pid  int `json:"pid"`
			}
			if json.Unmarshal(b, &si) == nil && si.Pid != 0 {
				log.Fatalf("端口 %d 监听失败: %v（server.json 记录 pid %d 端口 %d，可能已有实例在跑，客户端可直接附着）",
					c.Port, err, si.Pid, si.Port)
			}
		}
		log.Fatalf("端口 %d 监听失败: %v", c.Port, err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", c.Port)
	log.Printf("ezharness core 已启动：%s（数据目录 %s）", url, c.DataDir)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	a.stop()
}

/* buildRouter 装配一代完整的 controller/service/domain 栈（重启换代时重建）。 */
func (a *app) buildRouter() *gin.Engine {
	hub := domain.NewHub()
	service.BootstrapHub(hub) // 配置加载/落盘与活动会话恢复归 service 层
	a.mu.Lock()
	a.hub = hub
	a.mu.Unlock()

	// 调试模式随代际重载：设置页开关，EZ_MODEL_DUMP=1（dev.bat）启动期强制开启
	modeldump.SetEnabled(os.Getenv("EZ_MODEL_DUMP") == "1" || hub.SettingsSnapshot().DebugMode)

	// 共享终端(魔法看板):workDir 与 agent shell 一致;换代随 shutdownGeneration 重建
	termSvc := service.NewTerminalService(service.ResolveWorkDir(hub.SettingsSnapshot().WorkDir))
	a.setTerm(termSvc)

	// 共享浏览器桥(魔法看板):端无关转发层,跨代复用;真实浏览器是
	// desktop 壳的资产,经 /api/browser/bridge 接入
	if a.browser == nil {
		a.browser = service.NewBrowserService(service.ResolveWorkDir(hub.SettingsSnapshot().WorkDir))
	}

	// 系统级 MCP router(全局唯一):agent 的 mcp hook 与页面/API 调用共用
	// 同一连接池,跨代复用;换代按当代 mcp.json 重载列表
	if a.mcpAuth == nil {
		a.mcpAuth = service.NewMcpAuthManager(hub.Fsys)
	}
	if a.mcpRouter == nil {
		a.mcpRouter = service.NewMcpRouter(context.Background(), hub.Fsys, a.mcpAuth)
	} else {
		service.SyncMcpServers(context.Background(), a.mcpRouter, hub.Fsys, a.mcpAuth)
	}
	a.mcpAuth.SetRouter(a.mcpRouter)

	agents := &service.AgentService{Hub: hub, Term: termSvc, Browser: a.browser, McpRouter: a.mcpRouter, McpAuth: a.mcpAuth,
		Media: service.NewMediaService(hub)}
	topicsSvc := &service.TopicService{Hub: hub, Agents: agents}
	appsSvc := &service.AppsService{Fsys: hub.Fsys, Term: termSvc, Hub: hub}
	chatSvc := &service.ChatService{Hub: hub}
	agents.Callbacks = service.NewCallbackService(hub, agents, topicsSvc, appsSvc, chatSvc, hub.Fsys)
	agents.Assemble(hub.ActiveSession(), hub.SettingsSnapshot())
	a.setCallbacks(agents.Callbacks)
	agents.Callbacks.Start()

	appSvc := &service.AppService{
		Cfg:       a.snapshot,
		RestartFn: a.restart,
		FormatFn:  a.format,
		Boot:      &a.boot,
	}
	controllers := controller.Controllers{
		Auth:    &controller.AuthController{Auth: a.auth, Openapi: a.openapi},
		Session: &controller.SessionController{Svc: &service.SessionService{Hub: hub, Callbacks: agents.Callbacks, Agents: agents}},
		Chat:    &controller.ChatController{Svc: chatSvc, Memory: &service.MemoryService{Hub: hub}, Callbacks: agents.Callbacks},
		Settings: &controller.SettingsController{
			Settings: &service.SettingsService{Hub: hub, Agents: agents},
			Memory:   &service.MemoryService{Hub: hub},
		},
		Topics:    &controller.TopicController{Svc: topicsSvc},
		Mcp:       &controller.McpController{Svc: service.NewMcpService(hub.Fsys, a.mcpRouter, a.mcpAuth)},
		Apps:      &controller.AppsController{Svc: appsSvc},
		Callbacks: &controller.CallbackController{Svc: agents.Callbacks},
		App:       &controller.AppController{Svc: appSvc},
		Terminal:  &controller.TerminalController{Svc: termSvc},
		Browser:   &controller.BrowserController{Svc: a.browser},
		Workspace: &controller.WorkspaceController{Svc: service.NewWorkspaceService(hub.Fsys, hub)},
		Pinboard:  &controller.PinboardController{Svc: &service.PinboardService{Fsys: hub.Fsys}},
		Media: &controller.MediaController{Svc: agents.Media,
			Settings: &service.SettingsService{Hub: hub, Agents: agents}},
	}
	return controller.NewRouter(controllers, distFS(), a.auth, a.openapi)
}
