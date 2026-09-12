/*
魔法看板·共享浏览器桥端点:desktop 壳连入 /api/browser/bridge,承载
core ↔ desktop 的 browser_* 工具调用转发(service.BrowserService 协议)。
*/
package controller

import (
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"ezharness/core/internal/service"
)

type BrowserController struct {
	Svc *service.BrowserService
}

/*
Bridge GET /api/browser/bridge:desktop 桥连入(同源校验与终端 WS 一致),
handler 阻塞至桥断开。单连接语义:新连接顶替旧连接。
*/
func (c *BrowserController) Bridge(g *gin.Context) {
	conn, err := websocket.Accept(g.Writer, g.Request, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*"},
	})
	if err != nil {
		return
	}
	c.Svc.AttachBridge(conn)
}
