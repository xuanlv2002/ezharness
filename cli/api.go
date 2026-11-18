/*
会话 API 视图与 SSE 帧：/api/bootstrap（分支清单）、/api/sessions/:id
（历史消息）、/api/sessions/:id/events（实时帧）。
*/
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
)

/* branch 是分支清单条目（/api/bootstrap.branches）。 */
type branch struct {
	ID        string `json:"id"`
	LeafID    string `json:"leafId"`
	Title     string `json:"title"`
	Msgs      int    `json:"msgs"`
	Kind      string `json:"kind"` // new | fork | compress
	Running   bool   `json:"running"`
	Active    bool   `json:"active"`
	UpdatedAt int64  `json:"updatedAt"`
}

/* statusView 信息卡的模型行用（/api/bootstrap.status）。 */
type statusView struct {
	Model string `json:"model"`
}

type bootstrapView struct {
	SessionID string      `json:"sessionId"`
	LeafID    string      `json:"leafId"`
	Branches  []branch    `json:"branches"`
	Status    *statusView `json:"status"`
}

func (c *client) bootstrap() (*bootstrapView, error) {
	var b bootstrapView
	if err := c.callJSON(http.MethodGet, "/api/bootstrap", nil, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

/* historyMsg 是历史消息（/api/sessions/:id.messages；role=user/assistant/tool）。 */
type historyMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Err     string `json:"err,omitempty"`
}

type historyView struct {
	ID       string       `json:"id"`
	RootID   string       `json:"rootId"`
	Busy     bool         `json:"busy"`
	Messages []historyMsg `json:"messages"`
}

func (c *client) history(rootID string) (*historyView, error) {
	var h historyView
	if err := c.callJSON(http.MethodGet, "/api/sessions/"+rootID, nil, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

/* send 向分支发消息；409 = 已有轮次运行中。 */
func (c *client) send(rootID, text string) error {
	return c.callJSON(http.MethodPost, "/api/sessions/"+rootID+"/messages",
		map[string]any{"text": text}, nil)
}

/* activate 切换分支。 */
func (c *client) activate(id string) error {
	return c.callJSON(http.MethodPost, "/api/branches/"+id+"/activate", nil, nil)
}

/* newBranch 新建分支（返回分支根 ID）。 */
func (c *client) newBranch() (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := c.callJSON(http.MethodPost, "/api/branches/new", nil, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

/* ---- SSE ---- */

/* frame 是 /api/sessions/:id/events 的帧（forkId 非空 = 后台分身帧）。 */
type frame struct {
	Type   string          `json:"type"`
	Iter   int             `json:"iter"`
	ForkID string          `json:"forkId,omitempty"`
	Data   json.RawMessage `json:"data"`
}

// 需要关心的帧的解包结构（其余忽略）
type toolStartData struct {
	Name string `json:"name"`
}

type modelEndData struct {
	Content string `json:"content"`
}

type turnEndData struct {
	StopReason string `json:"stopReason"`
	Err        string `json:"err,omitempty"`
}

/*
sseStream 订阅分支事件流：回调在独立 goroutine 触发，连接错误经 errCb
上报。返回断开函数。
*/
func (c *client) sseStream(rootID string, onFrame func(frame), onErr func(error)) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	tr := &http.Transport{}
	hc := &http.Client{Transport: tr} // 流式长连接靠 ctx 取消，不设整体超时
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/sessions/"+rootID+"/events", nil)
	if err != nil {
		cancel()
		onErr(err)
		return func() {}
	}
	req.Header.Set("EzHarnessToken", c.token)

	go func() {
		res, err := hc.Do(req)
		if err != nil {
			if ctx.Err() == nil {
				onErr(err)
			}
			return
		}
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) < 6 || string(line[:6]) != "data: " {
				continue // 注释行（心跳）或空行
			}
			var f frame
			if json.Unmarshal(line[6:], &f) == nil {
				onFrame(f)
			}
		}
		if err := sc.Err(); err != nil && ctx.Err() == nil {
			onErr(err)
		}
	}()
	var stopped bool
	return func() {
		if stopped {
			return
		}
		stopped = true
		cancel() // 关连接：Scan 立即返回，goroutine 自行退出
		tr.CloseIdleConnections()
	}
}
