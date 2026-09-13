/*
前端产物嵌入单二进制（构建流程先 npm run build 产出 frontend/dist，
再拷贝到 core/web/dist 后编译；目录缺失则 go build 直接报错。
core/web/dist 是构建产物，不入库）。
*/
package main

import (
	"embed"
	"io/fs"
)

//go:embed web/dist
var dist embed.FS

/* distFS 返回以 web/dist 为根的静态资源。 */
func distFS() fs.FS {
	sub, err := fs.Sub(dist, "web/dist")
	if err != nil {
		return nil
	}
	return sub
}
