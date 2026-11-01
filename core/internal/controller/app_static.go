/*
快应用静态页伺服：apps/ 下的文件原样返回。页内调用 core API 走 OpenAPI
面（/api/open/*，见 open-api 技能与 /api/open/sdk.js），鉴权由页面自行
处理，core 不改写任何页面内容。
*/
package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/service"
)

/* serveApps 服务 apps/ 下静态文件。 */
func serveApps() gin.HandlerFunc {
	server := http.FileServer(http.Dir(service.AppsDir))
	return func(g *gin.Context) {
		g.Request.URL.Path = g.Param("filepath")
		server.ServeHTTP(g.Writer, g.Request)
	}
}
