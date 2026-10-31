/*
OpenAPI 浏览器 SDK（GET /api/open/sdk.js）：免鉴权伺服、跨源放行。页面
一行引入后 ez.config 传令牌，即可 ez.model.call / ez.mcp.call 调核心能力。
SDK 文件本身不含任何秘密，令牌只经 ez.config 进入。
*/
package controller

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed sdk.js
var sdkJS []byte

/* serveSdk 返回 SDK 脚本（Content-Type 由 gin Data 写死，禁缓存免得重置令牌后旧 SDK 行为漂移）。 */
func serveSdk(g *gin.Context) {
	g.Header("Cache-Control", "no-store")
	g.Data(http.StatusOK, "application/javascript; charset=utf-8", sdkJS)
}
