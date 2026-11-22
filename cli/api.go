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
	"net/url"
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

/* statusView 是 /api/status 与本会话状态（bootstrap.status）的共用视图。 */
type statusView struct {
	Model         string `json:"model"`
	ApproveMode   string `json:"approveMode"`
	SessionID     string `json:"sessionId"`
	RootID        string `json:"rootId"`
	SessionMsgs   int    `json:"sessionMsgs"`
	ContextTokens int    `json:"contextTokens"`
	ContextWindow int    `json:"contextWindow"`
	Busy          bool   `json:"busy"`
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
	Role      string `json:"role"`
	Content   string `json:"content"`
	Reasoning string `json:"reasoning,omitempty"`
	Err       string `json:"err,omitempty"`
	ToolCalls []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"tool_calls,omitempty"`
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

/* sendFile 是消息附件：整文件引用，core 校验路径存在且不是目录。 */
type sendFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

/* send 向分支发消息（dir = 发起端目录，随轮进 agent_status 作软提示）；409 = 已有轮次运行中。 */
func (c *client) send(rootID, text, dir string, files []sendFile) error {
	body := map[string]any{"text": text}
	if dir != "" {
		body["dir"] = dir
	}
	if len(files) > 0 {
		body["files"] = files
	}
	return c.callJSON(http.MethodPost, "/api/sessions/"+rootID+"/messages", body, nil)
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

/* status 拉活动会话状态。 */
func (c *client) status() (*statusView, error) {
	var st statusView
	if err := c.callJSON(http.MethodGet, "/api/status", nil, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

/* modelEntry 是 /api/models 主模型条目（只取选择需要的字段）。 */
type modelEntry struct {
	Name     string `json:"name"`
	Vision   bool   `json:"vision"`
	Disabled bool   `json:"disabled"`
}

func (c *client) models() ([]modelEntry, error) {
	var out struct {
		Main []modelEntry `json:"main"`
	}
	if err := c.callJSON(http.MethodGet, "/api/models", nil, &out); err != nil {
		return nil, err
	}
	return out.Main, nil
}

/* prefsView 是会话偏好更新的回执。 */
type prefsView struct {
	ApproveMode string `json:"approveMode"`
	Model       string `json:"model"`
}

func (c *client) setPrefs(rootID string, patch map[string]any) (prefsView, error) {
	var out prefsView
	err := c.callJSON(http.MethodPost, "/api/sessions/"+rootID+"/prefs", patch, &out)
	return out, err
}

/* compact 归档当前话题（总结归档并开新会话）。 */
func (c *client) compact(rootID string) error {
	return c.callJSON(http.MethodPost, "/api/topics/compact", map[string]any{"rootId": rootID}, nil)
}

/* ---- 人机决策 ---- */

func (c *client) decideApprove(rootID, callID string, approve bool, reason string) error {
	return c.callJSON(http.MethodPost, "/api/sessions/"+rootID+"/decisions/approve",
		map[string]any{"callId": callID, "approve": approve, "reason": reason}, nil)
}

func (c *client) decideAnswer(rootID, callID, input string) error {
	return c.callJSON(http.MethodPost, "/api/sessions/"+rootID+"/decisions/answer",
		map[string]any{"callId": callID, "input": input}, nil)
}

func (c *client) decidePlan(rootID, callID, action, feedback string) error {
	return c.callJSON(http.MethodPost, "/api/sessions/"+rootID+"/decisions/plan",
		map[string]any{"callId": callID, "action": action, "feedback": feedback}, nil)
}

func (c *client) decideMcpAuth(rootID, callID string, ok bool, reason string) error {
	return c.callJSON(http.MethodPost, "/api/sessions/"+rootID+"/decisions/mcp-auth",
		map[string]any{"callId": callID, "ok": ok, "reason": reason}, nil)
}

func (c *client) oauthStart(name, metadataURL string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	err := c.callJSON(http.MethodPost, "/api/mcp/oauth/start",
		map[string]any{"name": name, "metadata_url": metadataURL}, &out)
	return out.URL, err
}

func (c *client) oauthStatus(name string) (bool, error) {
	var out struct {
		Authorized bool `json:"authorized"`
	}
	err := c.callJSON(http.MethodGet, "/api/mcp/oauth/status?name="+url.QueryEscape(name), nil, &out)
	return out.Authorized, err
}

func (c *client) oauthCancel(name string) error {
	return c.callJSON(http.MethodPost, "/api/mcp/oauth/cancel", map[string]any{"name": name}, nil)
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
type modelEndData struct {
	Content   string `json:"content"`
	Reasoning string `json:"reasoning,omitempty"`
	ToolCalls []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"toolCalls,omitempty"`
}

type toolStartData struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type toolEndData struct {
	CallID string `json:"callId"`
	Name   string `json:"name"`
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
