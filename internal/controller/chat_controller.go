/* ChatController：消息发送、取消、决策回传与 SSE 事件流。 */
package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
	"ezharness/internal/service"
)

/* 附件输入限制（拖入即暂存，发送只传 tmp/ 路径引用）。 */
const maxAttachFiles = 8 // 单条消息附件数上限

/* ChatController 对话表现层。 */
type ChatController struct {
	Svc *service.ChatService
}

/*
	SendMessage POST /api/sessions/:id/messages（:id=分支根 ID）。

文本与引用至少其一；引用统一为工作目录内路径（一切皆资源：拖入暂存
的 tmp 文件、画板编辑的图片、AI 生成的文件均可引用）——files 是整
文件引用（附件 chips），refs 带标注片段（文件页「添加到对话」），均
经 <reference_file> 记录告知模型。路径必须落在工作目录内且文件存在
——工作目录本就是 AI 沙箱，引用无越权面。
*/
func (c *ChatController) SendMessage(g *gin.Context) {
	var body struct {
		Text  string `json:"text"`
		Files []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"files"`
		Refs []struct {
			Path  string `json:"path"`
			Items []struct {
				Sel  string `json:"sel"`
				Note string `json:"note"`
				From int    `json:"from"`
				To   int    `json:"to"`
			} `json:"items"`
		} `json:"refs"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Text) == "" && len(body.Files) == 0 && len(body.Refs) == 0 {
		g.JSON(http.StatusBadRequest, gin.H{"error": "text required"})
		return
	}
	if len(body.Files)+len(body.Refs) > maxAttachFiles {
		g.JSON(http.StatusBadRequest, gin.H{"error": "引用过多（单条最多 8 个）"})
		return
	}
	workDir := service.ResolveWorkDir(c.Svc.Hub.SettingsSnapshot().WorkDir)
	checkPath := func(name, p string) (string, bool) {
		abs, err := filepath.Abs(filepath.FromSlash(p))
		if err != nil || p == "" {
			g.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("引用 %s 路径无效", name)})
			return "", false
		}
		rel, err := filepath.Rel(workDir, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			g.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("引用 %s 不在工作目录内", name)})
			return "", false
		}
		if info, err := os.Stat(abs); err != nil || info.IsDir() {
			g.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("引用 %s 不存在或已失效", name)})
			return "", false
		}
		return filepath.ToSlash(abs), true
	}
	refs := make([]hooks.RefFile, 0, len(body.Files)+len(body.Refs))
	for _, f := range body.Files {
		abs, ok := checkPath(f.Name, f.Path)
		if !ok {
			return
		}
		refs = append(refs, hooks.RefFile{Path: abs})
	}
	for _, r := range body.Refs {
		abs, ok := checkPath(filepath.Base(filepath.FromSlash(r.Path)), r.Path)
		if !ok {
			return
		}
		items := make([]hooks.RefItem, 0, len(r.Items))
		for _, it := range r.Items {
			items = append(items, hooks.RefItem{Sel: it.Sel, Note: it.Note, From: it.From, To: it.To})
		}
		refs = append(refs, hooks.RefFile{Path: abs, Items: items})
	}
	if err := c.Svc.Send(g.Param("id"), body.Text, refs); err != nil {
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

/* CancelTurn POST /api/sessions/:id/cancel（:id=分支根 ID）。 */
func (c *ChatController) CancelTurn(g *gin.Context) {
	c.Svc.Cancel(g.Param("id"))
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/*
	Events GET /api/sessions/:id/events（SSE，:id=分支根 ID——按分支路由，

连接时重放该分支的回放帧与未决人机请求；compact 换代对象不变不断线）。
*/
func (c *ChatController) Events(g *gin.Context) {
	sess := c.Svc.Hub.SessionOf(g.Param("id"))
	if sess == nil {
		sess = c.Svc.Hub.Active
	}
	fl, ok := g.Writer.(interface{ Flush() })
	if !ok {
		g.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}
	g.Header("Content-Type", "text/event-stream")
	g.Header("Cache-Control", "no-cache")
	g.Header("Connection", "keep-alive")
	fmt.Fprint(g.Writer, ": connected\n\n")

	/* 建连首帧：当前轮运行态（前端复位 busy / 截断本地本轮块，配合
	随后的整轮回放干净重建——防断线重连导致的重复块与卡死） */
	if data, err := json.Marshal(domain.ReplaySync(sess.TurnActive())); err == nil {
		fmt.Fprintf(g.Writer, "data: %s\n\n", data)
	}
	for _, frame := range sess.ReplayFrames() {
		fmt.Fprintf(g.Writer, "data: %s\n\n", frame)
	}
	fl.Flush()

	ch, unsub := sess.Subscribe()
	defer unsub()

	hb := g.Request.Context()
	// 心跳注释帧：SSE 经代理（Vite dev proxy 等）空闲约 200s 被断，
	// 周期性写入保持连接活性（注释行不触发前端 onmessage）。
	hbTick := time.NewTicker(20 * time.Second)
	defer hbTick.Stop()
	for {
		select {
		case <-hb.Done():
			return
		case <-hbTick.C:
			if _, err := fmt.Fprint(g.Writer, ": hb\n\n"); err != nil {
				return
			}
			fl.Flush()
		case frame := <-ch:
			if _, err := fmt.Fprintf(g.Writer, "data: %s\n\n", frame); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

/* DecideApprove POST /api/sessions/:id/decisions/approve（:id=分支根 ID）。 */
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
	c.Svc.DecideApprove(g.Param("id"), body.CallID, body.Approve, body.Reason)
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* Notifications GET /api/notifications：全分支未决人机请求汇总（通知栏
全局轮询数据源；纯读无状态，后台分支的请求也在此可达）。组间按最新
请求时间排序（Hub.Sessions 遍历序随机），保证轮询结果顺序稳定。 */
func (c *ChatController) Notifications(g *gin.Context) {
	type noticeGroup struct {
		RootID string                 `json:"rootId"`
		Items  []domain.PendingNotice `json:"items"`
	}
	out := []noticeGroup{}
	for _, s := range c.Svc.Hub.Sessions() {
		if items := s.PendingNotices(); len(items) > 0 {
			out = append(out, noticeGroup{RootID: s.Root(), Items: items})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Items[0].Ts > out[j].Items[0].Ts
	})
	g.JSON(http.StatusOK, out)
}

/* DecideAnswer POST /api/sessions/:id/decisions/answer（:id=分支根 ID）。 */
func (c *ChatController) DecideAnswer(g *gin.Context) {
	var body struct {
		CallID string `json:"callId" binding:"required"`
		Input  string `json:"input"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Svc.DecideAnswer(g.Param("id"), body.CallID, body.Input)
	g.JSON(http.StatusOK, gin.H{"ok": true})
}
