/*
AppController 是应用级端点：health（代际探活）、配置查询、换代重启、
格式化。凭证管理端点在 auth.go。
*/
package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/service"
)

type AppController struct {
	Svc *service.AppService
}

/*
Health 探活：前端在重启后轮询，boot 与 restart 响应一致即新代就绪。
换端口重启时旧页面在新 origin 上轮询本端点，跨域响应需放行。
*/
func (c *AppController) Health(g *gin.Context) {
	g.Header("Access-Control-Allow-Origin", "*")
	st := c.Svc.Status()
	g.JSON(http.StatusOK, gin.H{"ok": true, "boot": st.Boot, "version": st.Version})
}

/* Config 返回设置页完整配置（端口/数据目录/路径清单/记忆三路径）。 */
func (c *AppController) Config(g *gin.Context) {
	g.JSON(http.StatusOK, c.Svc.ConfigView())
}

/*
UpdateCheck GET /api/app/update-check：查 GitHub 最新 release（带超时）。
只回最新版本与发布页，是否可升级由前端拿本地版本比对。
*/
func (c *AppController) UpdateCheck(g *gin.Context) {
	info, err := c.Svc.LatestRelease(g.Request.Context())
	if err != nil {
		g.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, info)
}

/* Restart 换代重启：port 零值不改；失败时旧服务不受影响。 */
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

/*
Format POST /api/app/format：清空全部配置与会话数据、恢复首次运行状态
（不可恢复）。响应 {url, boot}，前端轮询 health.boot 就绪后重载页面。
*/
func (c *AppController) Format(g *gin.Context) {
	res, err := c.Svc.Format()
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, res)
}
