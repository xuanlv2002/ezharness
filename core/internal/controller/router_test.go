package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/service"
)

/* newTestAuth 在临时数据目录里备好默认密码并签发一枚会话（cwd 即数据目录）。 */
func newTestAuth(t *testing.T) (*service.AuthStore, string) {
	t.Chdir(t.TempDir())
	a := service.LoadAuthState()
	a.EnsureDefaultPassword()
	s, err := a.Login(service.DefaultPassword, "测试端", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return a, s.Token
}

func TestWsToken(t *testing.T) {
	cases := []struct {
		header string
		token  string
	}{
		{"", ""},
		{"agent-auth", ""},
		{"agent-auth, abc123", "abc123"},
		{"foo, agent-auth, tok", ""}, /* 标记必须排首位 */
		{"abc123, agent-auth", ""},
	}
	for _, c := range cases {
		if got := wsToken(c.header); got != c.token {
			t.Fatalf("wsToken(%q) = %q, want %q", c.header, got, c.token)
		}
	}
}

func TestSessionAuthChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth, token := newTestAuth(t)
	r := gin.New()
	r.GET("/api/x", sessionAuth(auth), func(g *gin.Context) { g.Status(http.StatusNoContent) })

	cases := []struct {
		name    string
		url     string
		headers map[string]string
		want    int
	}{
		{"header", "/api/x", map[string]string{sessionTokenHeader: token}, http.StatusNoContent},
		{"cookie", "/api/x", map[string]string{"Cookie": authCookieName + "=" + token}, http.StatusNoContent},
		{"query rejected", "/api/x?token=" + token, nil, http.StatusUnauthorized},
		{"ws subprotocol", "/api/x", map[string]string{"Sec-WebSocket-Protocol": "agent-auth, " + token}, http.StatusNoContent},
		/* WS 握手能连上发命令，cookie 不豁免 */
		{"ws upgrade with cookie rejected", "/api/x", map[string]string{"Cookie": authCookieName + "=" + token, "Upgrade": "websocket"}, http.StatusUnauthorized},
		{"ws upgrade with header", "/api/x", map[string]string{sessionTokenHeader: token, "Upgrade": "websocket"}, http.StatusNoContent},
		{"ws upgrade with subprotocol", "/api/x", map[string]string{"Sec-WebSocket-Protocol": "agent-auth, " + token, "Upgrade": "websocket"}, http.StatusNoContent},
		{"valid header with stale cookie", "/api/x", map[string]string{sessionTokenHeader: token, "Cookie": authCookieName + "=nope"}, http.StatusNoContent},
		{"stale header with valid cookie", "/api/x", map[string]string{sessionTokenHeader: "nope", "Cookie": authCookieName + "=" + token}, http.StatusNoContent},
		{"missing", "/api/x", nil, http.StatusUnauthorized},
		{"bad subprotocol", "/api/x", map[string]string{"Sec-WebSocket-Protocol": "agent-auth, nope"}, http.StatusUnauthorized},
		{"bad cookie", "/api/x", map[string]string{"Cookie": authCookieName + "=nope"}, http.StatusUnauthorized},
		/* 会话面不认 openapi 令牌 */
		{"openapi token rejected on session route", "/api/x", map[string]string{openapiTokenHeader: "op1"}, http.StatusUnauthorized},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.url, nil)
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != c.want {
			t.Fatalf("%s: status = %d, want %d", c.name, w.Code, c.want)
		}
	}
}

func TestOpenGuardChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth, session := newTestAuth(t)
	openapi := service.LoadOpenapiStore()
	op := openapi.Create("脚本")
	r := gin.New()
	r.GET("/api/open/x", openGuard(auth, openapi), func(g *gin.Context) { g.Status(http.StatusNoContent) })

	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"openapi token", map[string]string{openapiTokenHeader: op.Token}, http.StatusNoContent},
		{"session header", map[string]string{sessionTokenHeader: session}, http.StatusNoContent},
		{"session cookie", map[string]string{"Cookie": authCookieName + "=" + session}, http.StatusNoContent},
		{"stale openapi with valid session", map[string]string{openapiTokenHeader: "stale", sessionTokenHeader: session}, http.StatusNoContent},
		{"stale openapi only", map[string]string{openapiTokenHeader: "stale"}, http.StatusUnauthorized},
		{"missing", nil, http.StatusUnauthorized},
		{"revoked openapi", map[string]string{openapiTokenHeader: revokedToken(t, openapi)}, http.StatusUnauthorized},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/open/x", nil)
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != c.want {
			t.Fatalf("%s: status = %d, want %d", c.name, w.Code, c.want)
		}
	}
	/* 每笔 openapi 调用（含被拒的）都落审计，且状态码是真实响应码 */
	entries := service.ReadOpenapiAuditTail(0)
	if len(entries) != len(cases) {
		t.Fatalf("audit entries = %d, want %d", len(entries), len(cases))
	}
	for _, e := range entries {
		if e.Status != http.StatusNoContent && e.Status != http.StatusUnauthorized {
			t.Fatalf("audit status = %d（被拒的调用应记 401）: %+v", e.Status, e)
		}
		if e.Status == http.StatusUnauthorized && e.TokenName != "" && e.TokenName != "会话" {
			t.Fatalf("被拒调用不该带令牌名: %+v", e)
		}
	}
}

/* revokedToken 造一枚已吊销的令牌。 */
func revokedToken(t *testing.T, o *service.OpenapiStore) string {
	t.Helper()
	tok := o.Create("待吊销")
	if err := o.Revoke(tok.ID); err != nil {
		t.Fatal(err)
	}
	return tok.Token
}

/*
cookie 只做只读资产传输：GET 可以，POST 必须带显式请求头（挡同 host

	其他端口页面借浏览器发起的 CSRF）。WebSocket 握手是 GET，不受影响。
*/
func TestSessionCookieReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth, token := newTestAuth(t)
	r := gin.New()
	r.GET("/api/x", sessionAuth(auth), func(g *gin.Context) { g.Status(http.StatusNoContent) })
	r.POST("/api/x", sessionAuth(auth), func(g *gin.Context) { g.Status(http.StatusNoContent) })

	do := func(method string, headers map[string]string) int {
		req := httptest.NewRequest(method, "/api/x", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	cookie := map[string]string{"Cookie": authCookieName + "=" + token}
	header := map[string]string{sessionTokenHeader: token}
	if got := do(http.MethodGet, cookie); got != http.StatusNoContent {
		t.Fatalf("cookie GET = %d, want 204", got)
	}
	if got := do(http.MethodPost, cookie); got != http.StatusUnauthorized {
		t.Fatalf("cookie POST = %d, want 401", got)
	}
	if got := do(http.MethodPost, header); got != http.StatusNoContent {
		t.Fatalf("header POST = %d, want 204", got)
	}
}

/* 登录拒绝非本机来源：跨站页面不能换会话令牌，DNS 重绑定也过不去。 */
func TestLocalOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name, origin, host string
		want               bool
	}{
		{"无 Origin（cli/desktop）", "", "127.0.0.1:5260", true},
		{"同源本机页面", "http://127.0.0.1:5260", "127.0.0.1:5260", true},
		{"localhost 页面", "http://localhost:5260", "localhost:5260", true},
		{"vite 代理", "http://localhost:5173", "localhost:5173", true},
		{"外部网站", "https://evil.example", "127.0.0.1:5260", false},
		{"本机其他端口", "http://127.0.0.1:3000", "127.0.0.1:5260", false},
		{"DNS 重绑定", "http://evil.example:5260", "evil.example:5260", false},
		{"file:// 页面", "null", "127.0.0.1:5260", false},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.Host = c.host
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		w := httptest.NewRecorder()
		g, _ := gin.CreateTestContext(w)
		g.Request = req
		if got := localOrigin(g); got != c.want {
			t.Fatalf("%s: localOrigin = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCorsOpenPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.OPTIONS("/api/open/*path", corsOpen())
	r.GET("/api/open/x", corsOpen(), func(g *gin.Context) { g.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodOptions, "/api/open/x", nil)
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, "+openapiTokenHeader)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("ACAO = %q, want *", got)
	}
	if allow := w.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(allow, openapiTokenHeader) {
		t.Fatalf("Allow-Headers misses %s: %q", openapiTokenHeader, allow)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/open/x", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if got := w2.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("GET ACAO = %q, want *", got)
	}
}

func TestSetAuthCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	setAuthCookie(c, "abc123")

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	ck := cookies[0]
	if ck.Name != authCookieName || ck.Value != "abc123" || ck.Path != "/" || !ck.HttpOnly ||
		ck.SameSite != http.SameSiteStrictMode || ck.MaxAge <= 0 {
		t.Fatalf("unexpected cookie: %+v", ck)
	}
}
