/*
PinboardController：便签板整块读写（GET/POST /api/pinboard）与便签图片
上传/回读（POST/GET /api/pinboard/images）。布局与交互全在前端，后端只
做形状校验与落盘。
*/
package controller

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
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

/* SaveImage POST /api/pinboard/images：请求体即图片原始字节，返回 {url}。 */
func (c *PinboardController) SaveImage(g *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(g.Request.Body, service.MaxPinboardImageBytes+1))
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "图片读取失败"})
		return
	}
	if len(body) > service.MaxPinboardImageBytes {
		g.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": fmt.Sprintf("图片超过 %dMB 上限", service.MaxPinboardImageBytes>>20)})
		return
	}
	url, err := c.Svc.SaveImage(g.Request.Context(), body)
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"url": url})
}

/* Image GET /api/pinboard/images/:name：按名回读图片。 */
func (c *PinboardController) Image(g *gin.Context) {
	data, mime, err := c.Svc.ReadImage(g.Request.Context(), g.Param("name"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			g.Status(http.StatusNotFound)
			return
		}
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	/* 文件名随机且内容不可变：长缓存，重复打开便签不重复拉图 */
	g.Header("Cache-Control", "private, max-age=31536000, immutable")
	g.Header("X-Content-Type-Options", "nosniff")
	g.Data(http.StatusOK, mime, data)
}
