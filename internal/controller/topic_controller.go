/* TopicController：话题存档查询与恢复。 */
package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/internal/domain"
	"ezharness/internal/service"
)

/* TopicController 话题表现层。 */
type TopicController struct {
	Svc *service.TopicService
}

/* List GET /api/topics。 */
func (c *TopicController) List(g *gin.Context) {
	g.JSON(http.StatusOK, c.Svc.List())
}

/* Get GET /api/topics/:id。 */
func (c *TopicController) Get(g *gin.Context) {
	d, err := c.Svc.Get(g.Request.Context(), g.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrTopicNotFound) {
			g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, d)
}

/* Delete DELETE /api/topics/:id（删整条线，运行中拒绝）。 */
func (c *TopicController) Delete(g *gin.Context) {
	if err := c.Svc.Delete(g.Request.Context(), g.Param("id")); err != nil {
		switch {
		case errors.Is(err, domain.ErrBusy):
			g.JSON(http.StatusConflict, gin.H{"error": "分支运行中，先停止再删除"})
		case errors.Is(err, service.ErrTopicNotFound):
			g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* NewBranch POST /api/branches/new（开新线）。 */
func (c *TopicController) NewBranch(g *gin.Context) {
	s := c.Svc.NewBranch()
	g.JSON(http.StatusOK, gin.H{"id": s.RootID})
}

/* Compact POST /api/topics/compact body {rootId?}（归档换代：指定分支总结归档开新篇，空=活动）。 */
func (c *TopicController) Compact(g *gin.Context) {
	var body struct {
		RootID string `json:"rootId"`
	}
	_ = g.ShouldBindJSON(&body)
	if err := c.Svc.Compact(g.Request.Context(), body.RootID); err != nil {
		switch {
		case errors.Is(err, service.ErrTopicNotFound):
			g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, domain.ErrBusy):
			g.JSON(http.StatusConflict, gin.H{"error": "会话运行中或归档进行中，稍后再试"})
		default:
			g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/*
	ForkBranch POST /api/sessions/:id/fork body {anchor}。

从 :id 源会话的第 anchor 条消息（含）复制前缀开新线。
*/
func (c *TopicController) ForkBranch(g *gin.Context) {
	var body struct {
		Anchor int `json:"anchor" binding:"required"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "anchor required"})
		return
	}
	s, err := c.Svc.Fork(g.Request.Context(), g.Param("id"), body.Anchor)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTopicNotFound):
			g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrBadAnchor):
			g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	g.JSON(http.StatusOK, gin.H{"id": s.RootID})
}

/* Activate POST /api/branches/:id/activate（切换分支，无 Busy 拒绝）。 */
func (c *TopicController) Activate(g *gin.Context) {
	if err := c.Svc.Switch(g.Request.Context(), g.Param("id")); err != nil {
		if errors.Is(err, service.ErrTopicNotFound) {
			g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"id": c.Svc.Hub.Active.RootID})
}

/* Tree GET /api/memory/tree（完整会话树：全部世代与分叉，记忆页渲染）。 */
func (c *TopicController) Tree(g *gin.Context) {
	g.JSON(http.StatusOK, c.Svc.Tree(g.Request.Context()))
}

/*
	Archive POST /api/sessions/:id/archive body {archived}（手动归档预留，

缺省 true=归档；活动/运行中的当前叶拒绝）。
*/
func (c *TopicController) Archive(g *gin.Context) {
	var body struct {
		Archived *bool `json:"archived"`
	}
	_ = g.ShouldBindJSON(&body)
	want := true
	if body.Archived != nil {
		want = *body.Archived
	}
	if err := c.Svc.Archive(g.Request.Context(), g.Param("id"), want); err != nil {
		switch {
		case errors.Is(err, service.ErrTopicNotFound):
			g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrCantArchive):
			g.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "archived": want})
}
