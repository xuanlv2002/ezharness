/*
ezharness core：Go sidecar（ezharness-core.exe），基于 ezloop 内核的
harness 后端（gin + 三层 MVC）。启动即对本机全权（文件不限目录 +
shell），在哪启动操作哪台设备。

桌面形态由 desktop/（Electron 壳）拉起本进程：主窗口（含工作区抽屉
的终端/资源/浏览器页）加载本进程伺服的页面；也可直接运行（浏览器访问的 web 端形态，浏览器
控制等桌面专属能力自动降级）。端口与数据目录经应用根 ezharness.json
配置（缺失自动创建），可在设置页修改并进程内重启（换代）。

配置记录（models.json/settings.json/mcp.json）与数据（sessions/
memory.md/topics.json）全部在数据目录，零配置可启动，apiKey 在
设置页配置。

分层：controller（表现）→ service（用例）→ domain（会话聚合与事件），
tools/hooks 为领域扩展，osfs/config 为基础设施。main 只做装配。
*/
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/config"
	"ezharness/core/internal/controller"
	"ezharness/core/internal/domain"
	"ezharness/core/internal/service"
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

	a, err := newApp(c)
	if err != nil {
		log.Fatal(err)
	}
	if err := a.start(); err != nil {
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
	a.mu.Lock()
	a.hub = hub
	a.mu.Unlock()

	// 共享终端(魔法看板):workDir 与 agent shell 一致;换代随 shutdownGeneration 重建
	termSvc := service.NewTerminalService(service.ResolveWorkDir(hub.SettingsSnapshot().WorkDir))
	a.setTerm(termSvc)

	// 共享浏览器桥(魔法看板):端无关转发层,跨代复用;真实浏览器是
	// desktop 壳的资产,经 /api/browser/bridge 接入
	if a.browser == nil {
		a.browser = service.NewBrowserService(service.ResolveWorkDir(hub.SettingsSnapshot().WorkDir))
	}

	agents := &service.AgentService{Hub: hub, Term: termSvc, Browser: a.browser}
	agents.Assemble(hub.Active, hub.SettingsSnapshot())

	appSvc := &service.AppService{
		Cfg:       a.snapshot,
		RestartFn: a.restart,
		Boot:      &a.boot,
	}
	controllers := controller.Controllers{
		Session: &controller.SessionController{Svc: &service.SessionService{Hub: hub}},
		Chat:    &controller.ChatController{Svc: &service.ChatService{Hub: hub}},
		Settings: &controller.SettingsController{
			Settings: &service.SettingsService{Hub: hub, Agents: agents},
			Memory:   &service.MemoryService{Hub: hub},
		},
		Topics:    &controller.TopicController{Svc: &service.TopicService{Hub: hub, Agents: agents}},
		Mcp:       &controller.McpController{Svc: service.NewMcpService(hub.Fsys)},
		Apps:      &controller.AppsController{Svc: &service.AppsService{Fsys: hub.Fsys}},
		App:       &controller.AppController{Svc: appSvc},
		Terminal:  &controller.TerminalController{Svc: termSvc},
		Browser:   &controller.BrowserController{Svc: a.browser},
		Workspace: &controller.WorkspaceController{Hub: hub},
	}
	return controller.NewRouter(controllers, distFS())
}
