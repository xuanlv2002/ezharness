/*
ezharness：基于 ezloop 内核的 harness 应用（gin + 三层 MVC）。
启动即对本机全权（文件不限目录 + shell），在哪启动操作哪台设备。

桌面软件形态：单二进制内嵌前端，启动即开原生窗口（WebView），
后端进程内运行；端口与数据目录经应用根 ezharness.json 配置（缺失
自动创建），可在设置页修改并进程内重启（换代）。EZHARNESS_NO_WINDOW=1
回落纯 server 模式（浏览器访问）。

配置记录（models.json/settings.json/mcp.json）与数据（sessions/
memory.md/topics.json）全部在数据目录，零配置可启动，apiKey 在
设置页配置。

分层：controller（表现）→ service（用例）→ domain（会话聚合与事件），
tools/hooks 为领域扩展，osfs/config 为基础设施。main 只做装配。
*/
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"ezharness/internal/config"
	"ezharness/internal/controller"
	"ezharness/internal/domain"
	"ezharness/internal/service"
)

func main() {
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
	log.Printf("ezharness 已启动：%s（数据目录 %s）", url, c.DataDir)

	if os.Getenv("EZHARNESS_NO_WINDOW") != "" {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		a.stop()
		return
	}
	openWindow(url)
	a.stop()
}

/* buildRouter 装配一代完整的 controller/service/domain 栈（重启换代时重建）。 */
func (a *app) buildRouter() *gin.Engine {
	hub := domain.NewHub()

	agents := &service.AgentService{Hub: hub}
	agents.Assemble(hub.Active, hub.ModelSnapshot(), hub.SettingsSnapshot())

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
		Topics: &controller.TopicController{Svc: &service.TopicService{Hub: hub}},
		App:    &controller.AppController{Svc: appSvc},
	}
	return controller.NewRouter(controllers, distFS())
}
