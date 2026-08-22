//go:build windows

/*
window 是桌面窗口壳（Windows）：go-webview2 纯 syscall 驱动 WebView2，
无需 CGO——本机 go build 即可出包（Win11 自带 WebView2 运行时）。
*/
package main

import (
	"fmt"
	"syscall"

	"github.com/jchv/go-webview2"
)

/* openWindow 打开主窗口并阻塞至窗口关闭。 */
func openWindow(url string) {
	enablePerMonitorDPI()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     true,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "ezharness",
			Width:  1360,
			Height: 900,
			Center: true,
		},
	})
	if w == nil {
		fmt.Println("WebView2 初始化失败（需要 Windows 10+ 与 WebView2 运行时）")
		return
	}
	defer w.Destroy()
	w.SetSize(1360, 900, webview2.HintNone)
	w.Navigate(url)
	w.Run()
}

/* enablePerMonitorDPI 声明逐显示器 DPI 感知（PER_MONITOR_AWARE_V2）——
否则高分屏缩放下 WebView2 内容被系统位图拉伸，整页发糊。
重复声明/老系统失败时静默（无副作用）。 */
func enablePerMonitorDPI() {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("SetProcessDpiAwarenessContext")
	_, _, _ = proc.Call(^uintptr(3)) // -4 = DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
}
