/*
PinboardController：便签板整块读写（GET/POST /api/pinboard）。布局与
交互全在前端，后端只做形状校验与落盘。
*/
package controller

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/service"
)

/* PinboardController 便签板存取。 */
type PinboardController struct {
	Svc *service.PinboardService
}

/* Get GET /api/pinboard：整块板（首次使用返回空板）。 */
func (c *PinboardController) Get(g *gin.Context) {
	data, err := c.Svc.Get(g.Request.Context())
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

/* Save POST /api/pinboard：整块覆写（前端防抖提交）。 */
func (c *PinboardController) Save(g *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(g.Request.Body, service.MaxPinboardBytes+1))
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "请求体读取失败"})
		return
	}
	if len(body) > service.MaxPinboardBytes {
		g.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "便签板数据超过 2MB 上限"})
		return
	}
	if err := c.Svc.Save(g.Request.Context(), body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}
