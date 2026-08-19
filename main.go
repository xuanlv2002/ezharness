/*
ezharness：基于 ezloop 内核的 Web harness（gin + 三层 MVC）。
启动即对本机全权（文件不限目录 + shell），在哪启动操作哪台设备。

分层：controller（表现）→ service（用例）→ domain（会话聚合与事件），
tools/hooks 为领域扩展，osfs/config 为基础设施。main 只做装配。
*/
package main

import (
	"fmt"
	"log"
	"os"

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

	// 领域根：恢复活动会话
	hub := domain.NewHub(c)

	// 服务层装配（agent 需在会话上组装一次）
	agents := &service.AgentService{Hub: hub}
	agents.Assemble(hub.Active, hub.SettingsSnapshot())

	controllers := controller.Controllers{
		Session: &controller.SessionController{Svc: &service.SessionService{Hub: hub}},
		Chat:    &controller.ChatController{Svc: &service.ChatService{Hub: hub}},
		Settings: &controller.SettingsController{
			Settings: &service.SettingsService{Hub: hub, Agents: agents},
			Memory:   &service.MemoryService{Hub: hub},
		},
		Topics: &controller.TopicController{Svc: &service.TopicService{Hub: hub}},
	}

	gin.SetMode(gin.ReleaseMode)
	router := controller.NewRouter(controllers, distFS())

	wd, _ := os.Getwd()
	log.Printf("ezharness 已启动：http://localhost%s（工作目录 %s）", c.Addr, wd)
	if err := router.Run(c.Addr); err != nil {
		log.Fatal(err)
	}
}
