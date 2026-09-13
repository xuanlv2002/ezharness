/*
魔法看板·共享终端通道:单条 WebSocket 多路复用全部终端(帧带 id 路由),
REST 提供新建/清单/关闭(前端 UI 用)。hello 帧带各终端快照,重连即恢复
屏幕;终端创建/退出经 terminals 帧广播(AI 新建的终端自然推给前端)。
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

	"ezharness/core/internal/service"
)

/* wsTermIn 是 C→S 帧。 */
type wsTermIn struct {
	Type string `json:"type"` // input | resize
	ID   string `json:"id"`
	Data string `json:"data"` // input 的 b64 字节
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

/* wsTermOut 是 S→C 帧。 */
type wsTermOut struct {
	Type     string             `json:"type"` // hello | data | terminals | error
	Sessions []wsTermSessionOut `json:"sessions,omitempty"`
	Message  string             `json:"message,omitempty"`
}

type wsTermSessionOut struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Origin   string `json:"origin"`
	Exited   bool   `json:"exited"`
	LastCmd  string `json:"lastCmd"`
	Snapshot string `json:"snapshot"` // b64 输出快照(hello 恢复用)
}

type TerminalController struct {
	Svc *service.TerminalService
}

/*
Ws GET /api/terminal/ws:升级后先发 hello(清单+快照),再双泵循环——
读泵分发 input(走 UserInput 记用户命令行)/resize,写泵把广播帧写给
前端;任一方向出错即断开(handler 阻塞至断开,与 SSE 同模式)。
*/
func (c *TerminalController) Ws(g *gin.Context) {
	// dev 模式页面经 Vite 代理(Origin: localhost:5173),放行本机来源
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
				case "data":
					b, _ = json.Marshal(map[string]string{
						"type": "data", "id": f.ID,
						"data": base64.StdEncoding.EncodeToString(f.Data),
					})
				case "terminals":
					out := wsTermOut{Type: "terminals"}
					for _, t := range f.Sessions {
						out.Sessions = append(out.Sessions, wsTermSessionOut{
							ID: t.ID, Name: t.Name, Origin: t.Origin, Exited: t.Exited, LastCmd: t.LastCmd,
						})
					}
					b, _ = json.Marshal(out)
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

	// 读泵:input/resize 分发
	go func() {
		for {
			_, r, err := conn.Reader(ctx)
			if err != nil {
				errCh <- nil
				return
			}
			var in wsTermIn
			if err := json.NewDecoder(r).Decode(&in); err != nil {
				continue
			}
			switch in.Type {
			case "input":
				if data, derr := base64.StdEncoding.DecodeString(in.Data); derr == nil {
					_ = c.Svc.UserInput(in.ID, data)
				}
			case "resize":
				_ = c.Svc.Resize(in.ID, in.Cols, in.Rows)
			}
		}
	}()

	<-errCh
}

func (c *TerminalController) writeHello(ctx context.Context, conn *websocket.Conn) error {
	out := wsTermOut{Type: "hello"}
	for _, t := range c.Svc.List() {
		out.Sessions = append(out.Sessions, wsTermSessionOut{
			ID: t.ID, Name: t.Name, Origin: t.Origin, Exited: t.Exited, LastCmd: t.LastCmd,
			Snapshot: base64.StdEncoding.EncodeToString(c.Svc.Snapshot(t.ID)),
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

/* List GET /api/terminal/list:终端清单。 */
func (c *TerminalController) List(g *gin.Context) {
	g.JSON(http.StatusOK, c.Svc.List())
}

/* Create POST /api/terminal/create:新建终端(body{name})。 */
func (c *TerminalController) Create(g *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	_ = g.ShouldBindJSON(&body)
	info, err := c.Svc.Create(body.Name, "用户")
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, info)
}

/* Close POST /api/terminal/close:关闭终端(body{id})。 */
func (c *TerminalController) Close(g *gin.Context) {
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
