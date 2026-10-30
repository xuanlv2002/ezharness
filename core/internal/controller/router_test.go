package controller

import (
	"net/http"
	"net/http/httptest"
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
	r.GET("/api/x", authRequired(token), func(g *gin.Context) { g.Status(http.StatusNoContent) })

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
