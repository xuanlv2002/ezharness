/* ChatController：消息发送、取消、决策回传与 SSE 事件流。 */
package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ezharness/internal/domain"
	"ezharness/internal/service"
)

/* ChatController 对话表现层。 */
type ChatController struct {
	Svc *service.ChatService
}

/* SendMessage POST /api/sessions/:id/messages。 */
func (c *ChatController) SendMessage(g *gin.Context) {
	var body struct {
		Text string `json:"text" binding:"required"`
	}
	if err := g.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Text) == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "text required"})
		return
	}
	if err := c.Svc.Send(body.Text); err != nil {
		if errors.Is(err, domain.ErrBusy) {
			g.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, domain.ErrNoAPIKey) {
			g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* CancelTurn POST /api/sessions/:id/cancel。 */
func (c *ChatController) CancelTurn(g *gin.Context) {
	c.Svc.Cancel()
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* Events GET /api/sessions/:id/events（SSE，连接时重放未决人机请求）。 */
func (c *ChatController) Events(g *gin.Context) {
	sess := c.Svc.Hub.Active
	fl, ok := g.Writer.(interface{ Flush() })
	if !ok {
		g.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}
	g.Header("Content-Type", "text/event-stream")
	g.Header("Cache-Control", "no-cache")
	g.Header("Connection", "keep-alive")
	fmt.Fprint(g.Writer, ": connected\n\n")

	for _, frame := range sess.ReplayFrames() {
		fmt.Fprintf(g.Writer, "data: %s\n\n", frame)
	}
	fl.Flush()

	ch, unsub := sess.Subscribe()
	defer unsub()

	hb := g.Request.Context()
	for {
		select {
		case <-hb.Done():
			return
		case frame := <-ch:
			if _, err := fmt.Fprintf(g.Writer, "data: %s\n\n", frame); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

/* DecideApprove POST /api/sessions/:id/decisions/approve。 */
func (c *ChatController) DecideApprove(g *gin.Context) {
	var body struct {
		CallID  string `json:"callId" binding:"required"`
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Svc.DecideApprove(body.CallID, body.Approve, body.Reason)
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* DecideAnswer POST /api/sessions/:id/decisions/answer。 */
func (c *ChatController) DecideAnswer(g *gin.Context) {
	var body struct {
		CallID string `json:"callId" binding:"required"`
		Input  string `json:"input"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Svc.DecideAnswer(body.CallID, body.Input)
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* DecidePlan POST /api/sessions/:id/decisions/plan。 */
func (c *ChatController) DecidePlan(g *gin.Context) {
	var body struct {
		CallID string `json:"callId" binding:"required"`
		Kind   string `json:"kind"`
		Input  string `json:"input"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Svc.DecidePlan(body.CallID, body.Kind, body.Input)
	g.JSON(http.StatusOK, gin.H{"ok": true})
}
