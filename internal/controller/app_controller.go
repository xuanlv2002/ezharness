/*
AppController 是应用级端点：health（代际探活）、配置查询、换代重启。
*/
package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/internal/service"
)

type AppController struct {
	Svc *service.AppService
}

/* Health 探活：前端在重启后轮询，boot 与 restart 响应一致即新代就绪。 */
func (c *AppController) Health(g *gin.Context) {
	st := c.Svc.Status()
	g.JSON(http.StatusOK, gin.H{"ok": true, "boot": st.Boot})
}

/* Config 返回设置页完整配置（端口/数据目录/路径清单/记忆三路径）。 */
func (c *AppController) Config(g *gin.Context) {
	g.JSON(http.StatusOK, c.Svc.ConfigView())
}

/* Restart 换代重启：port/dataDir 零值不改；失败时旧服务不受影响。 */
func (c *AppController) Restart(g *gin.Context) {
	var req service.RestartRequest
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := c.Svc.Restart(req)
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, res)
}
