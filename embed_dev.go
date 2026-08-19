//go:build !release

/* dev 模式：不服务静态资源，前端走 Vite dev server（proxy /api）。 */
package main

import "io/fs"

/* distFS dev 模式恒返回 nil。 */
func distFS() fs.FS { return nil }
