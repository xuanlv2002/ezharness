/*
快应用静态页伺服：/apps/* 不鉴权（页面本身可被打开），但页内调用 core API
必须鉴权。新入口不再带令牌，同源请求靠 ez_token cookie；这里的令牌桥只兼容
旧链接（?token=）：从 URL 取令牌存进独立的 ez-app-token 键（不碰主界面的
ez-token），给页内 fetch 补 X-EZ-Token。令牌不写进页面源码；无令牌且无
cookie 时无法调用 API。
*/
package controller

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/service"
)

const appAuthBridge = `<script>
(function () {
  var KEY = 'ez-app-token';
  var q = new URLSearchParams(location.search);
  var t = q.get('token') || '';
  if (t) {
    sessionStorage.setItem(KEY, t);
    q.delete('token');
    var rest = q.toString();
    history.replaceState(null, '', location.pathname + (rest ? '?' + rest : ''));
  } else {
    t = sessionStorage.getItem(KEY) || '';
  }
  if (!t) return;
  var orig = window.fetch;
  window.fetch = function (input, init) {
    var raw = typeof input === 'string' ? input : (input && input.url) || '';
    var u;
    try { u = new URL(raw, location.href); } catch (e) { return orig(input, init); }
    if (u.origin !== location.origin || u.pathname.indexOf('/api/') !== 0) return orig(input, init);
    var headers = new Headers(init && init.headers);
    headers.set('X-EZ-Token', t);
    return orig(input, Object.assign({}, init, { headers: headers }));
  };
})();
</script>`

/*
	injectAppAuth 把令牌桥插到 </head> 前（无 head 则插到 <body> 开标签后，

再退化为追加到末尾——不放到最前，免得脚本把 doctype 挤掉进怪异模式）。
*/
func injectAppAuth(html string) string {
	lower := strings.ToLower(html)
	if i := strings.Index(lower, "</head>"); i >= 0 {
		return html[:i] + appAuthBridge + html[i:]
	}
	if i := strings.Index(lower, "<body"); i >= 0 {
		if j := strings.IndexByte(html[i:], '>'); j >= 0 {
			k := i + j + 1
			return html[:k] + appAuthBridge + html[k:]
		}
	}
	return html + appAuthBridge
}

/* serveApps 服务 apps/ 下静态文件；HTML 响应注入令牌桥。 */
func serveApps() gin.HandlerFunc {
	dir := http.Dir(service.AppsDir)
	server := http.FileServer(dir)
	return func(g *gin.Context) {
		path := g.Param("filepath")
		name := path
		if strings.HasSuffix(name, "/") {
			name += "index.html"
		}
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm") {
			if f, err := dir.Open(name); err == nil {
				defer f.Close()
				if st, serr := f.Stat(); serr == nil && !st.IsDir() {
					if data, rerr := io.ReadAll(f); rerr == nil {
						g.Data(http.StatusOK, "text/html; charset=utf-8", []byte(injectAppAuth(string(data))))
						return
					}
				}
			}
		}
		g.Request.URL.Path = path
		server.ServeHTTP(g.Writer, g.Request)
	}
}
