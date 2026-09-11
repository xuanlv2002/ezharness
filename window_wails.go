/*
window 是桌面窗口壳（Wails v3，跨平台）：无边框窗口 + 系统托盘 +
关闭最小化到托盘。

- 页面一律走本进程 gin 的真实网络地址（http://127.0.0.1:<port>）：
  不经 wails 资产桥——该桥在 Windows 上缓冲整个响应，SSE 等流式无法
  工作；走网络后桌面端与浏览器访问行为完全一致。
- 无边框拖拽/双击最大化走 WebView2 原生非客户区支持
  （NonClientRegionSupport + 前端 CSS app-region: drag），无需 JS 注入。
- 托盘常驻：左键切换窗口显示，右键菜单（打开/退出）。
- 关闭行为实时读设置：CloseToTray 开 = 隐藏到托盘不弹窗；关 = 前端
  关闭询问（页面 modal，勾选「以后最小化到托盘」即持久化）。Alt+F4/
  任务栏关闭走系统惯例直接退出。
*/
package main

import (
	_ "embed"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed assets/icon.png
var trayIcon []byte

var appWinSeq atomic.Int64 // 快应用子窗口命名序号

/* MinWindowW 主窗口最小宽：全开组合不裁切的下限——64 侧栏 + 656
主列布局下限 + 280 终端保底。再窄则时间线内容（代码块等）放不下，
会出横向滚动条。 */
const MinWindowW = 1000

/* clampToScreen 把窗口尺寸按主屏工作区等比钳制，返回可用尺寸。 */
func clampToScreen(win *application.WebviewWindow, w, h int) (int, int) {
	sc, err := win.GetScreen()
	if err != nil || sc == nil {
		return w, h
	}
	wa := sc.WorkArea
	if w <= wa.Width && h <= wa.Height {
		return w, h
	}
	scale := math.Min(float64(wa.Width)/float64(w), float64(wa.Height)/float64(h))
	return int(float64(w) * scale), int(float64(h) * scale)
}

/* openWindow 打开主窗口并阻塞至应用退出；返回后调用方收尾 server。
URL 走本进程真实网络地址（start 已完成 listen 后才开窗，无竞态）。 */
func openWindow(a *app) {
	opts := application.WebviewWindowOptions{
		Name:      "main",
		Title:     "ezharness",
		Width:     a.cfg.WindowW,
		Height:    a.cfg.WindowH,
		MinWidth:  MinWindowW,
		Hidden:    true, // 尺寸钳制后再显示，避免超大窗口闪现
		Frameless: true,
		// OS 文件拖放（默认 false = 拖入被拦截，HTML5 DnD 收不到；
		// 开启后 Wails 接管外部拖入，页面内 HTML5 拖放不受影响）
		EnableFileDrop: true,
		// 组合宿主 + 非客户区支持：前者让 WndProc 接入宿主命中路由
		// （边缘缩放 resizeBorderHitTest + app-region 拖拽命中），后者开启
		// WebView2 对 CSS app-region 的解析。二者缺一则边缘无法缩放。
		Windows: application.WindowsWindow{
			NonClientRegionSupport:    true,
			WebView2CompositionHosting: true,
		},
	}
	opts.URL = fmt.Sprintf("http://127.0.0.1:%d/?desktop=1", a.cfg.Port)
	wailsApp := application.New(application.Options{Name: "ezharness"})
	win := wailsApp.Window.NewWithOptions(opts)

	// Run 之前 Show() 是静默 no-op（窗口 impl 尚未创建），必须在应用
	// 启动后的事件里显示：页面加载完成 → 按屏幕工作区钳制尺寸 → Show
	// （Hidden 起步防超大窗口闪现；仅首次导航生效，刷新不重触发）
	var shown atomic.Bool
	show := func() {
		if !shown.CompareAndSwap(false, true) {
			return
		}
		if w, h := clampToScreen(win, a.cfg.WindowW, a.cfg.WindowH); w != a.cfg.WindowW || h != a.cfg.WindowH {
			win.SetSize(w, h)
		}
		win.Show()
		log.Printf("ezharness 窗口已显示（启动后 %.1fs）", time.Since(appStart).Seconds())
	}
	win.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) { show() })
	// NavigationCompleted 偶发丢失（WebView2 时序）会让 Hidden 窗口永远
	// 不显示：超时兜底，与事件路径经 CAS 幂等合流。若显示恒比事件
	// 晚 ~2s 即兜底触发（事件丢失），其余慢在 WebView2 初始化/页面加载
	time.AfterFunc(2*time.Second, show)

	// OS 文件拖放不在此处理（前端 window 级 HTML5 drop 统一 stash：
	// WebView2 下 dataTransfer.files 可用，与浏览器行为一致且无双通道
	// 去重问题）。EnableFileDrop 仍须开启——Wails 注入的 dragenter 在
	// 无 data-file-drop-target 命中时会设 dropEffect=none，drop 即被禁。

	// 关闭拦截：实时读设置决定隐藏或放行（CloseToTray 运行时生效）。
	// quitting 是退出意图（托盘退出/前端确认退出）：Quit() 会触发关窗流程，
	// 若仍走 CloseToTray 拦截会把退出取消掉（这是"托盘退出退不出"的另一
	// 半根因）。标题栏 X 的询问弹窗在前端（页面 modal），Alt+F4/任务栏
	// 关闭走系统惯例直接退出。
	var quitting atomic.Bool
	// shutdown 退出流程：置 quitting 让关窗放行 → 同步收尾（取消运行轮并
	// 落盘；无轮时毫秒级）→ Quit 正常走关窗退出；Quit 卡死时超时强退兜底
	shutdown := func() {
		quitting.Store(true)
		a.stop()
		wailsApp.Quit()
		time.Sleep(5 * time.Second)
		os.Exit(0)
	}
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if quitting.Load() || !a.hub.SettingsSnapshot().CloseToTray {
			return
		}
		win.Hide()
		e.Cancel()
	})
	a.winCtl.Set(wailsWindow{
		wailsApp: wailsApp,
		win:      win,
		port:     func() int { return a.snapshot().Port },
		quit:     shutdown,
	})

	tray := wailsApp.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTooltip("ezharness")
	tray.OnClick(func() {
		if win.IsVisible() {
			win.Hide()
		} else {
			win.Show()
			win.Focus()
		}
	})
	menu := application.NewMenu()
	menu.Add("打开 ezharness").OnClick(func(*application.Context) {
		win.Show()
		win.Focus()
	})
	menu.Add("退出").OnClick(func(*application.Context) { go shutdown() })
	tray.SetMenu(menu)

	log.Printf("窗口装配完成（启动后 %.1fs），初始化 WebView", time.Since(appStart).Seconds())
	_ = wailsApp.Run() // 阻塞主线程；退出（关窗/托盘退出）后返回
}

/* wailsWindow 适配 wails Window 到 controller.WindowControl（剥掉返回值）。 */
type wailsWindow struct {
	wailsApp *application.App
	win      *application.WebviewWindow
	port     func() int
	quit     func() // 退出流程（收尾落盘 + Quit；openWindow 闭包注入）
}

func (a wailsWindow) Minimise()         { a.win.Minimise() }
func (a wailsWindow) ToggleMaximise()   { a.win.ToggleMaximise() }
func (a wailsWindow) IsMaximised() bool { return a.win.IsMaximised() }
func (a wailsWindow) Hide()             { a.win.Hide() }

/* RequestQuit 真退出（前端关闭询问确认后调用）：quitting 置位让关窗
钩子放行，再走完整收尾退出流程。 */
func (a wailsWindow) RequestQuit() {
	if a.quit != nil {
		go a.quit()
	}
}
/* OpenAppWindow 为快应用开独立子窗口：页面走本进程 gin 直出的绝对 URL
（release 与 dev 一致；wails 资产域只服务主窗口相对路径）。带系统标题栏。
path 为完整 http(s) URL 时直接加载（看板浏览器的"独立窗口"，绕开 iframe
内嵌限制）。 */
func (a wailsWindow) OpenAppWindow(path, title string) {
	url := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		url = fmt.Sprintf("http://127.0.0.1:%d%s", a.port(), path)
	}
	opts := application.WebviewWindowOptions{
		Name:   fmt.Sprintf("app-%d", appWinSeq.Add(1)),
		Title:  title,
		URL:    url,
		Width:  1100,
		Height: 720,
		Hidden: true,
	}
	w := a.wailsApp.Window.NewWithOptions(opts)
	if cw, ch := clampToScreen(w, opts.Width, opts.Height); cw != opts.Width || ch != opts.Height {
		w.SetSize(cw, ch)
	}
	w.Show()
}
