/*
魔法看板·共享浏览器通道:单条 WebSocket 多路复用全部标签(帧带 tabId
路由),REST 提供新建/清单/关闭(前端 UI 用)。hello 帧带浏览器状态与
各标签最新帧,重连即有画面;标签/页面状态变化经 tabs 帧广播,镜像帧
走 frame 帧,浏览器启动/下载状态走 status 帧,真窗口唤起/收起走
window 帧。镜像仅供监视;页面操控由用户在唤起的真窗口内原生完成,
AI 经 browser_* 工具注入——控制帧(navigate/viewport/window)经
同一条 WS 回传。
*/
package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"ezharness/internal/service"
)

/* wsBrowserIn 是 C→S 帧(镜像控制:地址栏导航/视口跟随/真窗口唤起)。 */
type wsBrowserIn struct {
	Type        string  `json:"type"` // navigate | viewport | window
	TabID       string  `json:"tabId"`
	URL         string  `json:"url"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	ScaleFactor float64 `json:"scaleFactor"` // viewport 帧的 devicePixelRatio
	Show        bool    `json:"show"`        // window 帧:唤起/收起真窗口
}

/* wsBrowserOut 是 S→C 帧。 */
type wsBrowserOut struct {
	Type    string            `json:"type"` // hello | tabs | frame | status | window
	Status  *wsBrowserStatus  `json:"status,omitempty"`
	Tabs    []wsBrowserTabOut `json:"tabs,omitempty"`
	TabID   string            `json:"tabId,omitempty"`
	Data    string            `json:"data,omitempty"` // frame 帧 JPEG 的 base64
	Width   int               `json:"width,omitempty"`
	Height  int               `json:"height,omitempty"`
	Window  *wsBrowserWindow  `json:"window,omitempty"`
	Message string            `json:"message,omitempty"`
}

type wsBrowserWindow struct {
	Shown bool `json:"shown"` // 真窗口是否在屏幕内
}

type wsBrowserStatus struct {
	Phase   string `json:"phase"` // downloading | launching | ready | error
	Message string `json:"message"`
}

type wsBrowserTabOut struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Origin  string `json:"origin"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Loading bool   `json:"loading"`
	Frame   string `json:"frame,omitempty"` // 最新帧 JPEG 的 base64(hello 恢复用)
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
}

type BrowserController struct {
	Svc *service.BrowserService
}

/*
Ws GET /api/browser/ws:升级后先发 hello(浏览器状态+清单+各标签最新帧),
再双泵循环——读泵分发三种控制帧,写泵把广播帧写给前端;任一方向出错
即断开(handler 阻塞至断开,与终端 WS 同模式)。
*/
func (c *BrowserController) Ws(g *gin.Context) {
	conn, err := websocket.Accept(g.Writer, g.Request, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*"},
	})
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(g.Request.Context())
	defer cancel()
	defer conn.CloseNow()

	if err := c.writeHello(ctx, conn); err != nil {
		return
	}

	subs, unsub := c.Svc.Subscribe()
	defer unsub()

	errCh := make(chan error, 2)

	// 写泵:广播帧 → WS
	go func() {
		for {
			select {
			case <-ctx.Done():
				errCh <- nil
				return
			case f, ok := <-subs:
				if !ok {
					errCh <- nil
					return
				}
				var b []byte
				switch f.Type {
				case "frame":
					if f.Data == nil {
						continue
					}
					b, _ = json.Marshal(wsBrowserOut{
						Type: "frame", TabID: f.TabID, Width: f.Width, Height: f.Height,
						Data: base64.StdEncoding.EncodeToString(f.Data),
					})
				case "tabs":
					out := wsBrowserOut{Type: "tabs"}
					for _, t := range f.Tabs {
						out.Tabs = append(out.Tabs, wsBrowserTabOut{
							ID: t.ID, Name: t.Name, Origin: t.Origin,
							URL: t.URL, Title: t.Title, Loading: t.Loading,
						})
					}
					b, _ = json.Marshal(out)
				case "status":
					b, _ = json.Marshal(wsBrowserOut{
						Type: "status", Status: &wsBrowserStatus{Phase: f.Phase, Message: f.Message},
					})
				case "window":
					b, _ = json.Marshal(wsBrowserOut{
						Type: "window", Window: &wsBrowserWindow{Shown: f.Shown},
					})
				default:
					continue
				}
				wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(wctx, websocket.MessageText, b)
				wcancel()
				if err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	// 读泵:三种控制帧分发
	go func() {
		for {
			_, r, err := conn.Reader(ctx)
			if err != nil {
				errCh <- nil
				return
			}
			var in wsBrowserIn
			if err := json.NewDecoder(r).Decode(&in); err != nil {
				continue
			}
			switch in.Type {
			case "navigate":
				_ = c.Svc.NavigateTab(in.TabID, in.URL)
			case "viewport":
				_ = c.Svc.ResizeViewport(in.TabID, in.Width, in.Height, in.ScaleFactor)
			case "window":
				c.Svc.SetWindowShown(in.Show, in.TabID)
			}
		}
	}()

	<-errCh
}

func (c *BrowserController) writeHello(ctx context.Context, conn *websocket.Conn) error {
	out := wsBrowserOut{Type: "hello", Status: &wsBrowserStatus{Phase: "ready"},
		Window: &wsBrowserWindow{Shown: c.Svc.WindowShown()}}
	for _, t := range c.Svc.List() {
		tab := wsBrowserTabOut{
			ID: t.ID, Name: t.Name, Origin: t.Origin,
			URL: t.URL, Title: t.Title, Loading: t.Loading,
		}
		if data, width, height, ok := c.Svc.SnapshotFrame(t.ID); ok {
			tab.Frame = base64.StdEncoding.EncodeToString(data)
			tab.Width, tab.Height = width, height
		}
		out.Tabs = append(out.Tabs, tab)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

/* List GET /api/browser/list:标签清单。 */
func (c *BrowserController) List(g *gin.Context) {
	g.JSON(http.StatusOK, c.Svc.List())
}

/* Create POST /api/browser/create:新建标签(body{name,url})。 */
func (c *BrowserController) Create(g *gin.Context) {
	var body struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	_ = g.ShouldBindJSON(&body)
	info, err := c.Svc.CreateTab(body.Name, "用户", body.URL)
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, info)
}

/* Close POST /api/browser/close:关闭标签(body{id})。 */
func (c *BrowserController) Close(g *gin.Context) {
	var body struct {
		ID string `json:"id" binding:"required"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Svc.Close(body.ID)
	g.JSON(http.StatusOK, gin.H{"ok": true})
}
