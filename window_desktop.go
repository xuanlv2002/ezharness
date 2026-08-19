//go:build !windows

/*
window 是桌面窗口壳（macOS/Linux）：系统原生 WebView（WKWebView /
webkit2gtk），经 webview_go 驱动，构建需 CGO 与本机工具链。
*/
package main

import webview "github.com/webview/webview_go"

/* openWindow 打开主窗口并阻塞至窗口关闭。 */
func openWindow(url string) {
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle("ezharness")
	w.SetSize(1360, 900, webview.HintNone)
	w.Navigate(url)
	w.Run()
}
