/*
core 的 HTTP 客户端：所有请求带 EzHarnessToken。令牌存 CLI 自己的凭证
文件（用户配置目录 ezharness/session.json），首次使用终端引导设密码或
登录，凭证丢失重登即可。
*/
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
)

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

/* sessionDoc 记住上次连的服务器与令牌；换服务器 = 重新登录。 */
type sessionDoc struct {
	Server string `json:"server"`
	Token  string `json:"token"`
}

func loadSession() (server, token string) {
	data, err := os.ReadFile(tokenFile())
	if err != nil {
		return "", ""
	}
	var doc sessionDoc
	if json.Unmarshal(data, &doc) != nil {
		return "", ""
	}
	return doc.Server, doc.Token
}

func saveSession(server, token string) {
	p := tokenFile()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	data, _ := json.MarshalIndent(sessionDoc{Server: server, Token: token}, "", "  ")
	_ = os.WriteFile(p, data, 0o600)
}

/*
ensureAuth 返回可用客户端：已存令牌有效直接用；否则终端引导——未初始化
设密码（init），已初始化输密码登录；成功后令牌与服务器地址一起落盘。
交互登录先问服务器地址（回车用传入的默认）；--server 指定或管道/脚本不提问。
*/
func ensureAuth(base string, askAddr bool) *client {
	savedServer, token := loadSession()
	if token != "" && savedServer != "" && !sameServer(savedServer, base) &&
		!(isLocalBase(savedServer) && isLocalBase(base)) {
		token = "" // 令牌属于另一台 core
	}
	cl := newClient(base, token)
	if cl.callJSON(http.MethodGet, "/api/auth/sessions", nil, nil) == nil {
		return cl
	}

	if askAddr && term.IsTerminal(int(os.Stdin.Fd())) {
		addr := readLine(fmt.Sprintf("服务器 [%s]（回车默认）: ", displayAddr(base)))
		if addr != "" {
			next, err := normalizeServer(addr)
			if err != nil {
				fatalf("%v", err)
			}
			if !sameServer(next, base) {
				base = next
				cl = newClient(base, "")
			}
		}
	}

	var st struct {
		Initialized bool `json:"initialized"`
	}
	if res, err := cl.do(http.MethodGet, "/api/auth/status", nil); err != nil {
		fatalf("core 不可达（%s）: %v", displayAddr(base), err)
	} else {
		_ = json.NewDecoder(res.Body).Decode(&st)
		res.Body.Close()
	}

	var path, pw string
	if !st.Initialized {
		fmt.Println("首次使用：为 ezharness 设置密码（桌面端 / 浏览器 / 终端首次连接都要用它）")
		pw = readPassword("设置密码（≥6 位）: ")
		if readPassword("再输一次: ") != pw {
			fatalf("两次输入不一致")
		}
		path = "/api/auth/init"
	} else {
		pw = readPassword(fmt.Sprintf("ezharness 密码（%s）: ", displayAddr(base)))
		path = "/api/auth/login"
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := cl.callJSON(http.MethodPost, path, map[string]any{"password": pw, "client": "cli"}, &out); err != nil {
		fatalf("登录失败: %v", err)
	}
	cl.token = out.Token
	saveSession(base, out.Token)
	return cl
}

/* readLine 读一行带回声的输入（服务器地址用；非终端不应调用）。 */
func readLine(label string) string {
	fmt.Print(label)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}

/* readPassword 无回显读密码（非终端回退普通行读）。 */
func readPassword(label string) string {
	fmt.Print(label)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}
