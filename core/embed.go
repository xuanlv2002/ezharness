/*
前端产物嵌入单二进制（构建流程先 npm run build 产出 core/frontend/dist，
再拷贝到 core/web/dist 后编译；目录缺失则 go build 直接报错。
core/web/dist 是构建产物，不入库）。
*/
package main

import (
	"embed"
	"io/fs"
)

// all: 前缀：Vite 的共享 chunk 以 _ 开头（_commonjsHelpers 等），默认 embed 规则会跳过
//
//go:embed all:web/dist
var dist embed.FS

/* distFS 返回以 web/dist 为根的静态资源。 */
func distFS() fs.FS {
	sub, err := fs.Sub(dist, "web/dist")
	if err != nil {
		return nil
	}
	return sub
}
