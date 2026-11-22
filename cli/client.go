/*
core 的 HTTP 客户端：所有请求带 EzHarnessToken。令牌存 CLI 自己的凭证
文件（用户配置目录 ezharness/session.json）；当前阶段用默认密码静默登录，
不向用户询问密码。
*/
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

/* defaultPassword core 启动时备好的固定密码（三端隐式登录用）。 */
const defaultPassword = "ezharness"

/* client 是带会话令牌的 core API 客户端。 */
type client struct {
	base  string
	token string
	http  *http.Client
}

func newClient(base, token string) *client {
	return &client{
		base:  base,
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *client) do(method, path string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("EzHarnessToken", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

/* apiError 携带 HTTP 状态码（调用方按 409 等做分支）。 */
type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string { return e.Msg }

/* callJSON 请求并解包；非 2xx 返回带状态码的 apiError。 */
func (c *client) callJSON(method, path string, body, out any) error {
	res, err := c.do(method, path, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		msg := fmt.Sprintf("HTTP %d", res.StatusCode)
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return &apiError{Status: res.StatusCode, Msg: msg}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

/* ---- 凭证文件 ---- */

/* tokenFile CLI 自己的凭证文件（与 desktop 的分开，各自保存各自登录）。 */
func tokenFile() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "ezharness", "session.json")
}

/* sessionDoc 记住会话令牌（core 重启不换）。 */
type sessionDoc struct {
	Token string `json:"token"`
}

func loadSession() string {
	data, err := os.ReadFile(tokenFile())
	if err != nil {
		return ""
	}
	var doc sessionDoc
	if json.Unmarshal(data, &doc) != nil {
		return ""
	}
	return doc.Token
}

func saveSession(token string) {
	p := tokenFile()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	data, _ := json.MarshalIndent(sessionDoc{Token: token}, "", "  ")
	_ = os.WriteFile(p, data, 0o600)
}

/*
ensureAuth 返回可用客户端：已存令牌有效直接用；否则用默认密码静默登录，
令牌落盘。core 启动时已备好默认密码，不再向用户询问密码或地址。
*/
func ensureAuth(base string) *client {
	cl := newClient(base, loadSession())
	if cl.token != "" && cl.callJSON(http.MethodGet, "/api/auth/sessions", nil, nil) == nil {
		return cl
	}
	var out struct {
		Token string `json:"token"`
	}
	err := cl.callJSON(http.MethodPost, "/api/auth/login",
		map[string]any{"password": defaultPassword, "client": "cli"}, &out)
	if err != nil {
		fatalf("自动登录失败（%s）: %v", displayAddr(base), err)
	}
	cl.token = out.Token
	saveSession(out.Token)
	return cl
}
