package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWsProtocolToken(t *testing.T) {
	cases := []struct {
		header string
		token  string
		proto  string
	}{
		{"", "", ""},
		{"foo", "", ""},
		{"ez-token.abc123", "abc123", "ez-token.abc123"},
		{"foo, ez-token.abc123, bar", "abc123", "ez-token.abc123"},
		{"ez-token.", "", "ez-token."},
	}
	for _, c := range cases {
		token, proto := wsProtocolToken(c.header)
		if token != c.token || proto != c.proto {
			t.Fatalf("wsProtocolToken(%q) = (%q, %q), want (%q, %q)", c.header, token, proto, c.token, c.proto)
		}
	}
}

func TestAuthRequiredChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const token = "abc123"
	r := gin.New()
	r.GET("/api/x", authRequired(NewTokenStore(token, "")), func(g *gin.Context) { g.Status(http.StatusNoContent) })

	cases := []struct {
		name    string
		url     string
		headers map[string]string
		want    int
	}{
		{"header", "/api/x", map[string]string{"X-EZ-Token": token}, http.StatusNoContent},
		{"cookie", "/api/x", map[string]string{"Cookie": authCookieName + "=" + token}, http.StatusNoContent},
		{"query rejected", "/api/x?token=" + token, nil, http.StatusUnauthorized},
		{"ws subprotocol", "/api/x", map[string]string{"Sec-WebSocket-Protocol": "ez-token." + token}, http.StatusNoContent},
		{"ws subprotocol list", "/api/x", map[string]string{"Sec-WebSocket-Protocol": "foo, ez-token." + token}, http.StatusNoContent},
		{"valid header with stale cookie", "/api/x", map[string]string{"X-EZ-Token": token, "Cookie": authCookieName + "=nope"}, http.StatusNoContent},
		{"stale header with valid cookie", "/api/x", map[string]string{"X-EZ-Token": "nope", "Cookie": authCookieName + "=" + token}, http.StatusNoContent},
		{"missing", "/api/x", nil, http.StatusUnauthorized},
		{"bad subprotocol", "/api/x", map[string]string{"Sec-WebSocket-Protocol": "ez-token.nope"}, http.StatusUnauthorized},
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

func TestOpenAuthChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const session, openapi = "sess1", "op1"
	r := gin.New()
	r.GET("/api/open/x", openAuth(NewTokenStore(session, openapi)), func(g *gin.Context) { g.Status(http.StatusNoContent) })

	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"openapi token", map[string]string{openapiTokenHeader: openapi}, http.StatusNoContent},
		{"session header", map[string]string{"X-EZ-Token": session}, http.StatusNoContent},
		{"session cookie", map[string]string{"Cookie": authCookieName + "=" + session}, http.StatusNoContent},
		/* 硬编码令牌已重置：同源凭 cookie 仍可过（cookie 覆盖 config 的语义） */
		{"stale openapi with valid cookie", map[string]string{openapiTokenHeader: "stale", "Cookie": authCookieName + "=" + session}, http.StatusNoContent},
		{"stale openapi only", map[string]string{openapiTokenHeader: "stale"}, http.StatusUnauthorized},
		{"missing", nil, http.StatusUnauthorized},
		{"openapi on session scheme", map[string]string{"X-EZ-Token": openapi}, http.StatusUnauthorized},
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
	if ck.Name != authCookieName || ck.Value != "abc123" || ck.Path != "/" || !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected cookie: %+v", ck)
	}
}
