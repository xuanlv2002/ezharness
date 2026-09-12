/*
魔法看板·共享浏览器:AI(browser_* 工具)与用户操控同一批真实
Chromium 标签(全局共享,个人助手语义)。浏览器经 CDP(rod)以有头
模式常驻,窗口藏于屏幕外;页面画面以 screencast 帧流镜像到前端仅供
监视,用户接管经"唤起真窗口"完成——在真实浏览器窗口里原生操作,
与 AI 注入共写同一页面。浏览器实例惰性启动(首次建标签才拉起),
Chromium 二进制缺失时自动下载 rod managed 版本到用户缓存目录(状态
经 WS status 帧反馈)。
*/
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"image"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

/* BrowserTabInfo 是浏览器标签清单条目(WS/REST/AI browser_list 共用)。 */
type BrowserTabInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Origin  string `json:"origin"`  // 创建来源:"用户" / "AI"
	URL     string `json:"url"`     // 当前页面地址
	Title   string `json:"title"`   // 当前页面标题
	Loading bool   `json:"loading"` // 导航进行中
}

/* BrowserFrame 是 service → WS 订阅者的广播帧(controller 负责编码)。 */
type BrowserFrame struct {
	Type    string // tabs | frame | status | window
	TabID   string // frame 帧的标签 id
	Data    []byte // frame 帧的 JPEG 字节
	Width   int    // 帧图像像素宽高
	Height  int
	Tabs    []BrowserTabInfo // tabs 帧的清单
	Phase   string           // status: downloading | launching | ready | error
	Message string
	Shown   bool // window 帧:真窗口是否在屏幕内
}

const (
	browserSubsBuf       = 16                    // 每订阅者帧队列(帧体积大,满丢帧由 hello 快照兜底)
	browserFrameGap      = 45 * time.Millisecond // ack 节流间隔(压帧率 ~20fps,视频页防帧风暴)
	browserDefaultWidth  = 1280
	browserDefaultHeight = 800
	browserElementWait   = 5 * time.Second        // AI 按选择器等元素
	browserTitleDebounce = 200 * time.Millisecond // 导航状态防抖广播
	browserSettleWait    = 300 * time.Millisecond // 点击/按键后等副作用显现
	browserReadDefault   = 4000
	browserReadMax       = 20000
	browserShotQuality   = 80
)

/* ── 标签 ── */

/*
BrowserTab 是一个被控浏览器标签。mu 只保护内存状态(url/title/loading/
帧缓存/page 引用);CDP 调用一律锁外(rod 并发安全,持锁会被等待类
调用阻塞输入路径)。
*/
type BrowserTab struct {
	ID       string
	Name     string
	Origin   string
	targetID proto.TargetTargetID

	mu             sync.Mutex
	page           *rod.Page
	url            string
	title          string
	loading        bool
	frameData      []byte
	frameWidth     int
	frameHeight    int
	frameMeta      *proto.PageScreencastFrameMetadata
	lastAck        time.Time
	sameStreak     int // 连续相同帧计数(ack 退避依据)
	viewportWidth  int // 当前 CSS 视口(窗口客户区,resize 去重防抖动)
	viewportHeight int
}

func (tab *BrowserTab) info() *BrowserTabInfo {
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return &BrowserTabInfo{ID: tab.ID, Name: tab.Name, Origin: tab.Origin,
		URL: tab.url, Title: tab.title, Loading: tab.loading}
}

func (tab *BrowserTab) snapshot() (data []byte, width, height int) {
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.frameData == nil {
		return nil, 0, 0
	}
	return append([]byte(nil), tab.frameData...), tab.frameWidth, tab.frameHeight
}

func (tab *BrowserTab) hold() *rod.Page {
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return tab.page
}

/*
frameToPageCoordinate 帧图像坐标 → 视口 CSS 坐标。CDP Input 注入的
X/Y 本就是视口相对坐标(不含滚动偏移),换算只需物理帧→CSS 的缩放:
viewportWidth/Height 是 ResizeViewport 记录的权威视口,frameWidth/Height
是截图实际尺寸(jpegSize 解析)——不依赖 screencast metadata(触发帧被
缩略到 320px 后其 deviceWidth 已不可信,曾致点击全部偏移失准)。
*/
func (tab *BrowserTab) frameToPageCoordinate(x, y int) (float64, float64) {
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.frameWidth == 0 || tab.frameHeight == 0 || tab.viewportWidth == 0 || tab.viewportHeight == 0 {
		return float64(x), float64(y)
	}
	scaleX := float64(tab.viewportWidth) / float64(tab.frameWidth)
	scaleY := float64(tab.viewportHeight) / float64(tab.frameHeight)
	return float64(x) * scaleX, float64(y) * scaleY
}

/* viewportCenter 返回视口中心(滚动注入位置)。 */
func (tab *BrowserTab) viewportCenter() (int, int) {
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.frameWidth == 0 || tab.frameHeight == 0 {
		return browserDefaultWidth / 2, browserDefaultHeight / 2
	}
	return tab.frameWidth / 2, tab.frameHeight / 2
}

/* ── 服务 ── */

/* BrowserService 管理被控浏览器与全部标签(全局单例,随换代重建)。 */
type BrowserService struct {
	mu         sync.Mutex
	seq        int
	tabs       map[string]*BrowserTab
	subs       map[chan BrowserFrame]struct{}
	launch     *launcher.Launcher
	browser    *rod.Browser
	launching   chan struct{} // 进行中的启动(nil=未启动或已失败,closed=已就绪)
	launchErr   error
	lastAi      string // AI 最近使用的标签 id(无 tabId 参数时兜底)
	windowShown bool   // 真窗口是否在屏幕内(唤起/收起状态,hello 同步用)
	// 真窗口外框相对客户区的补偿(边框+标签栏+地址栏,视口跟随 resize 用);
	// 初始估值,resize 后读回实际视口自校准
	windowChromeWidth  int
	windowChromeHeight int
	seesImages         func() bool
	shotDir            string
}

/* NewBrowserService 构造(workDir 为 agent 工作区,截图落 workspace/browser)。 */
func NewBrowserService(workDir string) *BrowserService {
	return &BrowserService{
		tabs:               map[string]*BrowserTab{},
		subs:               map[chan BrowserFrame]struct{}{},
		windowChromeWidth:  16,
		windowChromeHeight: 88,
		shotDir:            filepath.Join(workDir, "browser"),
	}
}

/* SetVisionProbe 注入主模型视觉判定(Assemble 时调用,截图返回形态实时裁决)。 */
func (s *BrowserService) SetVisionProbe(fn func() bool) {
	s.mu.Lock()
	s.seesImages = fn
	s.mu.Unlock()
}

/* ModelSeesImages 主模型是否多模态(browser_screenshot 返回形态裁决,BrowserIO 实现)。 */
func (s *BrowserService) ModelSeesImages() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seesImages != nil && s.seesImages()
}

/* downloadLogger 把 rod 下载日志转发为 status 帧(前端覆盖层展示进度)。 */
type downloadLogger struct{ broadcast func(string) }

func (l downloadLogger) Println(vs ...any) { l.broadcast(fmt.Sprint(vs...)) }

/*
ensureBrowser 惰性启动被控浏览器:并发调用经 launching channel 串行等待,
只启动一次;失败后允许下次调用重试。
*/
func (s *BrowserService) ensureBrowser() error {
	s.mu.Lock()
	if s.browser != nil {
		s.mu.Unlock()
		return nil
	}
	ch := s.launching
	first := ch == nil
	if first {
		s.launching = make(chan struct{})
		ch = s.launching
	}
	s.mu.Unlock()
	if first {
		s.startBrowser(ch)
	}
	<-ch
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.launchErr
}

func (s *BrowserService) startBrowser(done chan struct{}) {
	defer close(done)
	s.broadcast(BrowserFrame{Type: "status", Phase: "downloading", Message: "正在下载 Chromium(首次运行,约 150MB)…"})
	managed := launcher.NewBrowser()
	managed.Logger = downloadLogger{broadcast: func(msg string) {
		if msg = strings.TrimSpace(msg); msg != "" {
			s.broadcast(BrowserFrame{Type: "status", Phase: "downloading", Message: msg})
		}
	}}
	bin, err := managed.Get()
	if err != nil {
		s.failLaunch(fmt.Errorf("下载 Chromium 失败: %w", err))
		return
	}
	s.broadcast(BrowserFrame{Type: "status", Phase: "launching", Message: "正在启动浏览器…"})
	l := launcher.New().
		Bin(bin).
		Leakless(false). // Windows Defender 常态拦截 leakless.exe(误报);进程清理由 Shutdown 的 BrowserClose 承担
		// 有头常驻(launcher.New 默认 headless,须显式关闭),但窗口创建即
		// 屏幕外(不闪窗打扰);镜像监视与 AI 注入均不依赖窗口可见,用户
		// 接管时再唤回屏幕内(SetWindowShown)
		Headless(false).
		Set("window-position", "-32000,-32000").
		Set("window-size", fmt.Sprintf("%d,%d", browserDefaultWidth, browserDefaultHeight)).
		// 屏幕外窗口会被 Windows 原生遮挡判定暂停渲染(screencast 随之停
		// 帧),显式禁掉全部后台节流与遮挡计算
		Set("disable-backgrounding-occluded-windows").
		Set("disable-renderer-backgrounding").
		Set("disable-background-timer-throttling").
		Set("disable-features", "CalculateNativeWinOcclusion").
		Set("disable-gpu")
	controlURL, err := l.Launch()
	if err != nil {
		s.failLaunch(fmt.Errorf("启动浏览器失败: %w", err))
		return
	}
	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		l.Cleanup()
		s.failLaunch(fmt.Errorf("连接浏览器失败: %w", err))
		return
	}
	s.mu.Lock()
	s.browser, s.launch, s.launchErr = browser, l, nil
	s.mu.Unlock()
	go s.watchTargets()
	go s.watchProcess()
	s.broadcast(BrowserFrame{Type: "status", Phase: "ready"})
}

func (s *BrowserService) failLaunch(err error) {
	s.mu.Lock()
	s.launchErr, s.launching = err, nil
	s.mu.Unlock()
	s.broadcast(BrowserFrame{Type: "status", Phase: "error", Message: err.Error()})
}

/*
watchTargets 感知标签销毁/崩溃(用户在页面内关闭、渲染器崩溃):
回调里不调 CDP,摘除动作转独立 goroutine。
*/
func (s *BrowserService) watchTargets() {
	s.mu.Lock()
	browser := s.browser
	s.mu.Unlock()
	if browser == nil {
		return
	}
	browser.EachEvent(
		func(e *proto.TargetTargetDestroyed) { go s.removeTabByTarget(e.TargetID) },
		func(e *proto.TargetTargetCrashed) { go s.removeTabByTarget(e.TargetID) },
	)()
}

/*
watchProcess 检测用户直接关闭真窗口:轻量 CDP 探活(进程退出即报错),
发现死亡后整体重置(下次建标签自动重新启动)。镜像监视与 AI 注入都
依赖浏览器存活,这里是被动的善后,不主动杀进程。
*/
func (s *BrowserService) watchProcess() {
	for {
		time.Sleep(2 * time.Second)
		s.mu.Lock()
		browser := s.browser
		s.mu.Unlock()
		if browser == nil {
			return
		}
		probe := proto.BrowserGetVersion{}
		if _, err := probe.Call(browser); err == nil {
			continue
		}
		s.Shutdown(0)
		s.broadcast(BrowserFrame{Type: "window", Shown: false})
		s.broadcastTabs()
		return
	}
}

/*
CreateTab 新建标签(id 形如 b1/b2);startURL 非空时异步导航(加载状态
经清单帧反馈)。name 空时取 startURL 域名或 id。
*/
func (s *BrowserService) CreateTab(name, origin, startURL string) (*BrowserTabInfo, error) {
	if err := s.ensureBrowser(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	browser := s.browser
	s.seq++
	id := fmt.Sprintf("b%d", s.seq)
	s.mu.Unlock()
	if browser == nil {
		return nil, fmt.Errorf("浏览器不可用")
	}

	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("新建标签失败: %w", err)
	}
	// 不预设 viewport override:页面用 --window-size 的自然视口,前端上报
	// 时第一次 override 一步到位(加载后再改 override 的双次变更会让
	// Input 注入坐标错位失效)
	if name == "" {
		name = id
		if parsed, perr := url.Parse(completeURL(startURL)); perr == nil && parsed.Host != "" {
			name = parsed.Host
		}
	}
	tab := &BrowserTab{ID: id, Name: name, Origin: origin, page: page, url: "about:blank", loading: startURL != "",
		viewportWidth: browserDefaultWidth, viewportHeight: browserDefaultHeight}
	if info, ierr := page.Info(); ierr == nil {
		tab.targetID = info.TargetID
	}
	// 激活为前台:CDP Input 对非前台页面的注入会被吞(浏览器自带初始
	// about:blank 一直占前台,不激活则镜像点击/键盘/悬浮全部无效)
	_ = proto.PageBringToFront{}.Call(page)
	s.mu.Lock()
	s.tabs[id] = tab
	s.mu.Unlock()

	tab.startScreencast(s)
	go tab.navigationPump(s)
	if startURL != "" && startURL != "about:blank" {
		go func() { _ = page.Navigate(completeURL(startURL)) }()
	}
	s.broadcastTabs()
	return tab.info(), nil
}

/*
startScreencast 启动帧流:screencast 只作"页面有变化"的触发器(320px 缩略
帧,编码开销极小,数据丢弃),收到信号即 captureScreenshot 出物理分辨率
高清帧——DSF 下 screencast 只有 CSS 分辨率,截图才有物理像素。headless
下 screencast 是"ack 后即出帧"(无变化抑制),静态页面也会连发触发帧,
故截图与上帧比对:相同不广播并逐级拉长 ack 间隔(45→720ms,五级退避),
有变化立即恢复全速——静止近零耗,动态 ~10fps。
*/
func (tab *BrowserTab) startScreencast(s *BrowserService) {
	go tab.hold().EachEvent(func(e *proto.PageScreencastFrame) {
		tab.mu.Lock()
		page := tab.page
		if e.Metadata != nil {
			tab.frameMeta = e.Metadata // 坐标换算依据(CSS 视口与滚动偏移)
		}
		gap := ackDelay(tab.sameStreak) - time.Since(tab.lastAck)
		tab.mu.Unlock()

		if gap > 0 {
			time.Sleep(gap)
		}
		_ = proto.PageScreencastFrameAck{SessionID: e.SessionID}.Call(page)
		tab.mu.Lock()
		tab.lastAck = time.Now()
		tab.mu.Unlock()

		shot, err := browserCapture(page, false)
		if err != nil || len(shot) == 0 {
			return
		}
		width, height := jpegSize(shot)
		if width == 0 || height == 0 {
			return
		}
		tab.mu.Lock()
		if bytes.Equal(tab.frameData, shot) {
			tab.sameStreak++
			tab.mu.Unlock()
			return
		}
		tab.sameStreak = 0
		tab.frameData = shot
		tab.frameWidth, tab.frameHeight = width, height
		data := append([]byte(nil), shot...)
		tab.mu.Unlock()
		s.broadcast(BrowserFrame{Type: "frame", TabID: tab.ID, Data: data, Width: width, Height: height})
	})()
	quality, triggerWidth, triggerHeight := 40, 320, 200
	cast := proto.PageStartScreencast{
		Format: proto.PageStartScreencastFormatJpeg, Quality: &quality,
		MaxWidth: &triggerWidth, MaxHeight: &triggerHeight,
	}
	_ = cast.Call(tab.hold())
}

/* ackDelay 连续相同帧的退避间隔:45/90/180/360/720ms(五级封顶)。 */
func ackDelay(streak int) time.Duration {
	if streak > 4 {
		streak = 4
	}
	return browserFrameGap << streak
}

/* jpegSize 解析 JPEG 宽高(帧尺寸标注,不用猜)。 */
func jpegSize(b []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

/*
navigationPump 跟踪页面导航与加载状态:主帧跳转更新 url/loading,加载
完成拉取标题,防抖广播清单(地址栏与清单的实时性来源)。
*/
func (tab *BrowserTab) navigationPump(s *BrowserService) {
	var timer *time.Timer
	schedule := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(browserTitleDebounce, func() {
			title, current := "", ""
			if info, err := tab.hold().Info(); err == nil {
				title, current = info.Title, info.URL
			}
			tab.mu.Lock()
			tab.title = title
			if current != "" {
				tab.url = current
			}
			tab.mu.Unlock()
			s.broadcastTabs()
		})
	}
	tab.hold().EachEvent(
		func(e *proto.PageFrameNavigated) {
			if e.Frame.ParentID != "" { // 只跟主帧
				return
			}
			tab.mu.Lock()
			tab.url, tab.loading = e.Frame.URL, true
			tab.mu.Unlock()
			schedule()
		},
		func(e *proto.PageLoadEventFired) {
			tab.mu.Lock()
			tab.loading = false
			tab.mu.Unlock()
			schedule()
		},
	)()
}

/* get 按 id 精确取标签(WS 输入路径必须带 id)。 */
func (s *BrowserService) get(id string) (*BrowserTab, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tab, ok := s.tabs[id]
	return tab, ok
}

/* resolve 取标签;id 为空时取 AI 最近使用的标签(工具参数兜底)并记录。 */
func (s *BrowserService) resolve(id string) (*BrowserTab, error) {
	s.mu.Lock()
	if id == "" {
		id = s.lastAi
	}
	tab, ok := s.tabs[id]
	if ok {
		s.lastAi = tab.ID
	}
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("浏览器标签 %q 不存在(可用 browser_list 查看)", id)
	}
	return tab, nil
}

/* List 返回全部标签清单(创建顺序)。 */
func (s *BrowserService) List() []BrowserTabInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]BrowserTabInfo, 0, len(s.tabs))
	for i := 1; i <= s.seq; i++ {
		if tab, ok := s.tabs[fmt.Sprintf("b%d", i)]; ok {
			out = append(out, *tab.info())
		}
	}
	return out
}

/*
ListBrowserTabsJSON 标签清单的 JSON 文本(browser_list 工具直接返回;
tools 侧不能引用 service 类型,走字符串解耦)。
*/
func (s *BrowserService) ListBrowserTabsJSON() string {
	b, err := json.Marshal(s.List())
	if err != nil {
		return "[]"
	}
	return string(b)
}

/* ── 用户镜像控制(WS 路径:地址栏导航/视口跟随/真窗口唤起) ── */

/*
NavigateTab 用户地址栏导航:scheme 缺省按 https:// 补全,不等待加载完成
(loading 状态经清单帧反馈)。
*/
func (s *BrowserService) NavigateTab(tabID, rawURL string) error {
	tab, ok := s.get(tabID)
	if !ok {
		return fmt.Errorf("浏览器标签 %q 不存在", tabID)
	}
	u := completeURL(rawURL)
	if err := tab.hold().Navigate(u); err != nil {
		return err
	}
	tab.mu.Lock()
	tab.url, tab.loading = u, true
	tab.mu.Unlock()
	s.broadcastTabs()
	return nil
}

/*
ResizeViewport 视口跟随镜像尺寸:有头模式下等价于把真窗口客户区调到
width×height——页面按"拉窄的浏览器窗口"布局,响应式与滚动条行为与真
窗口完全一致;物理分辨率由系统显示缩放决定,截图自然高清(scaleFactor
仅透传不参与窗口调整)。窗口外框=客户区+chrome 界面(边框/标签栏/地址
栏),补偿量经读回实际视口自校准。大窗打开期间用户自管窗口,跳过跟
随;收起时前端强制重报恢复。越界静默丢弃。
*/
func (s *BrowserService) ResizeViewport(tabID string, width, height int, scaleFactor float64) error {
	tab, ok := s.get(tabID)
	if !ok {
		return nil
	}
	if width < 200 || height < 200 || width > 3000 || height > 3000 {
		return nil
	}
	s.mu.Lock()
	shown := s.windowShown
	chromeWidth, chromeHeight := s.windowChromeWidth, s.windowChromeHeight
	browser := s.browser
	s.mu.Unlock()
	if shown {
		return nil
	}
	tab.mu.Lock()
	sameViewport := tab.viewportWidth == width && tab.viewportHeight == height
	tab.mu.Unlock()
	if !sameViewport && browser != nil {
		target := proto.TargetTargetID(tabID2Target(s, tabID))
		get := proto.BrowserGetWindowForTarget{TargetID: target}
		if info, err := get.Call(browser); err == nil {
			bounds := proto.BrowserBounds{Width: pointerOf(width + chromeWidth), Height: pointerOf(height + chromeHeight)}
			set := proto.BrowserSetWindowBounds{WindowID: info.WindowID, Bounds: &bounds}
			if err := set.Call(browser); err != nil {
				log.Printf("[browser] 视口跟随 resize 失败: %v", err)
			}
			// resize 异步应用,稍候读回实际视口:修正本 tab 记录并自校准补偿
			time.Sleep(150 * time.Millisecond)
			if res, err := tab.hold().Eval(`() => [window.innerWidth, window.innerHeight]`); err == nil {
				if arr := res.Value.Arr(); len(arr) == 2 {
					actualWidth, actualHeight := arr[0].Int(), arr[1].Int()
					if actualWidth > 0 && actualHeight > 0 {
						s.mu.Lock()
						if d := width - actualWidth; d != 0 {
							s.windowChromeWidth += d
						}
						if d := height - actualHeight; d != 0 {
							s.windowChromeHeight += d
						}
						s.mu.Unlock()
						width, height = actualWidth, actualHeight
					}
				}
			}
			tab.mu.Lock()
			tab.viewportWidth, tab.viewportHeight = width, height
			tab.mu.Unlock()
		}
	}
	// 视口上报即"前端正在显示此标签":激活为前台(非前台页面的 Input 注入
	// 会被吞);切换标签时尺寸常相同(去重跳过 resize),前台仍须切换
	_ = proto.PageBringToFront{}.Call(tab.hold())
	return nil
}

/*
SetWindowShown 唤起/收起真浏览器窗口:shown=true 移回屏幕内并激活指定
标签(用户在真实窗口里原生操作——点击/键盘/IME/视频全无镜像换算),
false 移回屏幕外(镜像监视与 AI 注入不受影响,屏幕外窗口仍保有前台
焦点语义,BringToFront 不会把它带到屏幕内)。窗口被用户直接关闭由
watchProcess 兜底重置。
*/
func (s *BrowserService) SetWindowShown(shown bool, tabID string) {
	s.mu.Lock()
	browser := s.browser
	s.windowShown = shown
	s.mu.Unlock()
	if browser != nil {
		// 必须带具体 target 查窗口:无参走"当前激活 target",常落在无
		// web contents 的初始页上,报 "No web contents in the target"
		target := proto.TargetTargetID(tabID2Target(s, tabID))
		get := proto.BrowserGetWindowForTarget{TargetID: target}
		info, getErr := get.Call(browser)
		if getErr != nil {
			log.Printf("[browser] GetWindowForTarget 失败: %v", getErr)
		} else {
			// BrowserBounds 的整数字段是 *int(CDP optional),统一 pointerOf 取址
			bounds := proto.BrowserBounds{Width: pointerOf(browserDefaultWidth), Height: pointerOf(browserDefaultHeight)}
			if info.Bounds != nil {
				bounds = *info.Bounds
			}
			if shown {
				bounds.Left, bounds.Top = pointerOf(60), pointerOf(60)
			} else {
				bounds.Left, bounds.Top = pointerOf(-32000), pointerOf(-32000)
			}
			bounds.WindowState = proto.BrowserWindowStateNormal
			set := proto.BrowserSetWindowBounds{WindowID: info.WindowID, Bounds: &bounds}
			if err := set.Call(browser); err != nil {
				log.Printf("[browser] SetWindowBounds(shown=%v) 失败: %v", shown, err)
			}
		}
	}
	if shown && tabID != "" {
		if tab, ok := s.get(tabID); ok {
			_ = proto.PageBringToFront{}.Call(tab.hold())
		}
	}
	s.broadcast(BrowserFrame{Type: "window", Shown: shown})
}

/* WindowShown 真窗口是否在屏幕内(hello 恢复用)。 */
func (s *BrowserService) WindowShown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.windowShown
}

/* tabID2Target 标签 id → CDP target id(空/未知时兜底第一个标签)。 */
func tabID2Target(s *BrowserService, tabID string) string {
	if tab, ok := s.get(tabID); ok && tab.targetID != "" {
		return string(tab.targetID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tab := range s.tabs {
		if tab.targetID != "" {
			return string(tab.targetID)
		}
	}
	return ""
}

/* pointerOf 整数取址(BrowserBounds 的 CDP optional 字段是 *int)。 */
func pointerOf(value int) *int {
	return &value
}

func completeURL(raw string) string {
	if raw == "" || strings.HasPrefix(raw, "about:") {
		return raw
	}
	if !strings.Contains(raw, "://") {
		return "https://" + raw
	}
	return raw
}

/* browserCapture 视口/整页截图(JPEG,质量 browserShotQuality)。 */
func browserCapture(page *rod.Page, fullPage bool) ([]byte, error) {
	quality := browserShotQuality
	return page.Screenshot(fullPage, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatJpeg, Quality: &quality,
	})
}

/*
KeyInput 按键注入:action=press 合成 down+up;key 用 DOM 键名
(Enter/Backspace/ArrowDown/F2…),组合键修饰位由调用方计算传入。
*/
func (s *BrowserService) KeyInput(tabID, action, key, code string, modifiers int) error {
	tab, ok := s.get(tabID)
	if !ok {
		return fmt.Errorf("浏览器标签 %q 不存在", tabID)
	}
	if code == "" {
		code = key
	}
	vk := browserKeyCode(key)
	ev := func(t proto.InputDispatchKeyEventType) error {
		return proto.InputDispatchKeyEvent{
			Type: t, Key: key, Code: code,
			WindowsVirtualKeyCode: vk, NativeVirtualKeyCode: vk, Modifiers: modifiers,
		}.Call(tab.hold())
	}
	switch action {
	case "down":
		return ev(proto.InputDispatchKeyEventTypeKeyDown)
	case "up":
		return ev(proto.InputDispatchKeyEventTypeKeyUp)
	case "press":
		if err := ev(proto.InputDispatchKeyEventTypeKeyDown); err != nil {
			return err
		}
		return ev(proto.InputDispatchKeyEventTypeKeyUp)
	}
	return fmt.Errorf("未知按键动作 %q", action)
}

/* TextInput 文本注入(InputInsertText 绕过键盘事件直接提交,输入框识别最稳)。 */
func (s *BrowserService) TextInput(tabID, text string) error {
	tab, ok := s.get(tabID)
	if !ok {
		return fmt.Errorf("浏览器标签 %q 不存在", tabID)
	}
	return proto.InputInsertText{Text: text}.Call(tab.hold())
}

/* ── AI 工具后端 ── */

/* StartBrowser 新建标签并导航,返回创建头行与页面标题。 */
func (s *BrowserService) StartBrowser(desc, rawURL string, timeoutMs int) (string, error) {
	timeout := time.Duration(clampInt(timeoutMs, 1000, 600000, 20000)) * time.Millisecond
	info, err := s.CreateTab(desc, "AI", "")
	if err != nil {
		return "", err
	}
	tab, _ := s.get(info.ID)
	head := fmt.Sprintf("[浏览器 #%s %q 已创建,画面镜像到魔法看板·浏览器页,用户可点\"打开大窗\"原生接管]", info.ID, info.Name)
	if rawURL == "" {
		return head, nil
	}
	title, loadErr := tab.load(completeURL(rawURL), timeout)
	if loadErr != nil {
		return head + "\n导航失败: " + loadErr.Error(), nil
	}
	if title == "" {
		title = "(无标题)"
	}
	return fmt.Sprintf("%s\n页面: %s", head, title), nil
}

/* load 导航并等待加载,返回页面标题(超时返回已得标题与提示)。 */
func (tab *BrowserTab) load(rawURL string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	page := tab.hold().Context(ctx)
	tab.mu.Lock()
	tab.loading = true
	tab.mu.Unlock()
	if err := page.Navigate(rawURL); err != nil {
		return "", err
	}
	_ = page.WaitLoad() // 超时或页面已加载过则返回,标题尽力而为
	title := ""
	if info, err := page.Info(); err == nil {
		title = info.Title
	}
	tab.mu.Lock()
	tab.loading = false
	tab.mu.Unlock()
	return title, nil
}

/* NavigateBrowser 跳转并等加载,返回新页面标题。 */
func (s *BrowserService) NavigateBrowser(tabID, rawURL string, timeoutMs int) (string, error) {
	tab, err := s.resolve(tabID)
	if err != nil {
		return "", err
	}
	timeout := time.Duration(clampInt(timeoutMs, 1000, 120000, 20000)) * time.Millisecond
	title, loadErr := tab.load(completeURL(rawURL), timeout)
	head := fmt.Sprintf("[浏览器 #%s %q]", tab.ID, tab.Name)
	if loadErr != nil {
		return head + "\n导航失败: " + loadErr.Error(), nil
	}
	if title == "" {
		title = "(无标题)"
	}
	return head + "\n页面: " + title, nil
}

/*
ClickBrowser 点击:selector 优先(等元素出现后点击);否则按视口截图像素
坐标换算点击(与镜像同源换算)。
*/
func (s *BrowserService) ClickBrowser(tabID, selector string, x, y int) (string, error) {
	tab, err := s.resolve(tabID)
	if err != nil {
		return "", err
	}
	if selector != "" {
		el, err := tab.hold().Timeout(browserElementWait).Element(selector)
		if err != nil {
			return "", fmt.Errorf("元素 %q 未找到: %w", selector, err)
		}
		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return "", fmt.Errorf("点击失败: %w", err)
		}
	} else {
		cssX, cssY := tab.frameToPageCoordinate(x, y)
		press := func(t proto.InputDispatchMouseEventType) error {
			return proto.InputDispatchMouseEvent{
				Type: t, X: cssX, Y: cssY, Button: proto.InputMouseButtonLeft, ClickCount: 1,
			}.Call(tab.hold())
		}
		if err := press(proto.InputDispatchMouseEventTypeMousePressed); err != nil {
			return "", err
		}
		if err := press(proto.InputDispatchMouseEventTypeMouseReleased); err != nil {
			return "", err
		}
	}
	time.Sleep(browserSettleWait) // 等点击副作用(跳转/弹层)初步显现
	return tab.stateLine(), nil
}

/* TypeBrowser 输入文本:selector 定位输入框(省略=当前焦点处),submit 回车提交。 */
func (s *BrowserService) TypeBrowser(tabID, selector, text string, submit bool) (string, error) {
	tab, err := s.resolve(tabID)
	if err != nil {
		return "", err
	}
	if selector != "" {
		el, err := tab.hold().Timeout(browserElementWait).Element(selector)
		if err != nil {
			return "", fmt.Errorf("输入框 %q 未找到: %w", selector, err)
		}
		if err := el.Input(text); err != nil {
			return "", fmt.Errorf("输入失败: %w", err)
		}
	} else if err := s.TextInput(tab.ID, text); err != nil {
		return "", err
	}
	if submit {
		if err := s.KeyInput(tab.ID, "press", "Enter", "Enter", 0); err != nil {
			return "", err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return tab.stateLine(), nil
}

/*
PressBrowserKey 按键/组合键:combo 如 "Enter"、"Control+A"、"Alt+F4"
(最后一段为主键,其余为修饰键)。
*/
func (s *BrowserService) PressBrowserKey(tabID, combo string) (string, error) {
	tab, err := s.resolve(tabID)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.TrimSpace(combo), "+")
	key := parts[len(parts)-1]
	modifiers := 0
	for _, mod := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(mod)) {
		case "alt", "option":
			modifiers |= 1
		case "control", "ctrl":
			modifiers |= 2
		case "meta", "cmd", "command", "os":
			modifiers |= 4
		case "shift":
			modifiers |= 8
		default:
			return "", fmt.Errorf("未知修饰键 %q", mod)
		}
	}
	if err := s.KeyInput(tab.ID, "press", key, key, modifiers); err != nil {
		return "", err
	}
	time.Sleep(browserSettleWait)
	return tab.stateLine(), nil
}

/* ScrollBrowser 页面滚动(direction=up|down,amountPx 滚动量默认 600)。 */
func (s *BrowserService) ScrollBrowser(tabID, direction string, amountPx int) (string, error) {
	tab, err := s.resolve(tabID)
	if err != nil {
		return "", err
	}
	if amountPx <= 0 {
		amountPx = 600
	}
	delta := -amountPx
	if direction == "up" {
		delta = amountPx
	}
	centerX, centerY := tab.viewportCenter()
	cssX, cssY := tab.frameToPageCoordinate(centerX, centerY)
	scroll := proto.InputDispatchMouseEvent{
		Type: proto.InputDispatchMouseEventTypeMouseWheel,
		X:    cssX, Y: cssY, DeltaY: float64(delta),
	}
	if err := scroll.Call(tab.hold()); err != nil {
		return "", err
	}
	time.Sleep(200 * time.Millisecond)
	return tab.stateLine(), nil
}

/*
ReadBrowser 读页面内容:mode=text 返回正文(innerText),mode=links 返回
链接清单(text→href)。chars 截尾(默认 4000,上限 20000)。
*/
func (s *BrowserService) ReadBrowser(tabID, mode string, chars int) (string, error) {
	tab, err := s.resolve(tabID)
	if err != nil {
		return "", err
	}
	chars = clampInt(chars, 100, browserReadMax, browserReadDefault)
	head := fmt.Sprintf("[浏览器 #%s %q]", tab.ID, tab.Name)
	if mode == "links" {
		res, err := tab.hold().Eval(`() => Array.from(document.querySelectorAll('a')).slice(0, 200)
			.map(a => (a.innerText.trim().slice(0,60) || '(无文字)') + ' → ' + a.href).join('\n')`)
		if err != nil {
			return "", fmt.Errorf("读取链接失败: %w", err)
		}
		return head + "\n" + prefixRunes(res.Value.Str(), chars), nil
	}
	res, err := tab.hold().Eval(`() => document.body ? document.body.innerText : '(空页面)'`)
	if err != nil {
		return "", fmt.Errorf("读取正文失败: %w", err)
	}
	return head + "\n" + prefixRunes(res.Value.Str(), chars), nil
}

/*
ScreenshotBrowser 截图:落盘 workspace/browser/<tabID>.jpg(覆写)。主
模型有视觉时返回 image_loaded 标记(由 filetools 转持久化图片消息),
无视觉时返回路径文字引导(防大图进无多模态的上下文)。
*/
func (s *BrowserService) ScreenshotBrowser(tabID string, fullPage bool) (string, error) {
	tab, err := s.resolve(tabID)
	if err != nil {
		return "", err
	}
	shot, err := browserCapture(tab.hold(), fullPage)
	if err != nil {
		return "", fmt.Errorf("截图失败: %w", err)
	}
	if err := os.MkdirAll(s.shotDir, 0o755); err != nil {
		return "", fmt.Errorf("创建截图目录失败: %w", err)
	}
	path := filepath.Join(s.shotDir, tab.ID+".jpg")
	if err := os.WriteFile(path, shot, 0o644); err != nil {
		return "", fmt.Errorf("写截图失败: %w", err)
	}
	head := fmt.Sprintf("[浏览器 #%s %q 截图已生成]", tab.ID, tab.Name)
	if !s.ModelSeesImages() {
		return head + "\n已保存至 " + path + ";当前模型无多模态能力,如需识别请在设置·模型启用视觉或图片识别槽", nil
	}
	return head + "\n" + `<image_loaded path="` + path + `"/>`, nil
}

/* CloseBrowserTab 关闭标签(工具路径);幂等。 */
func (s *BrowserService) CloseBrowserTab(tabID string) error {
	tab, err := s.resolve(tabID)
	if err != nil {
		return err
	}
	s.removeTab(tab.ID)
	return nil
}

func (tab *BrowserTab) stateLine() string {
	tab.mu.Lock()
	defer tab.mu.Unlock()
	state := fmt.Sprintf("[浏览器 #%s %q] 页面: %s", tab.ID, tab.Name, tab.title)
	if tab.loading {
		state += "(加载中)"
	}
	return state
}

func prefixRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n…(过长已截断)"
}

/* ── 摘除与收尾 ── */

func (s *BrowserService) removeTab(id string) {
	s.mu.Lock()
	tab, ok := s.tabs[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	delete(s.tabs, id)
	if s.lastAi == id {
		s.lastAi = ""
	}
	s.mu.Unlock()
	_ = tab.hold().Close()
	s.broadcastTabs()
}

/* removeTabByTarget 按 CDP target 摘除(页面内关闭/崩溃路径,target 已销毁不再调 Close)。 */
func (s *BrowserService) removeTabByTarget(targetID proto.TargetTargetID) {
	s.mu.Lock()
	var tab *BrowserTab
	for _, t := range s.tabs {
		if t.targetID == targetID {
			tab = t
			break
		}
	}
	if tab == nil {
		s.mu.Unlock()
		return
	}
	delete(s.tabs, tab.ID)
	if s.lastAi == tab.ID {
		s.lastAi = ""
	}
	s.mu.Unlock()
	s.broadcastTabs()
}

/* Close 关闭一个标签(REST 路径);幂等。 */
func (s *BrowserService) Close(id string) {
	if tab, ok := s.get(id); ok {
		s.removeTab(tab.ID)
	}
}

/*
Shutdown 换代/停机收尾:BrowserClose 让被控浏览器整体退出(全部标签、
渲染进程随之销毁),launcher.Cleanup 等待进程退出并清理临时 profile。
*/
func (s *BrowserService) Shutdown(_ time.Duration) {
	s.mu.Lock()
	browser, launch := s.browser, s.launch
	s.browser, s.launch, s.launching, s.launchErr = nil, nil, nil, nil
	s.tabs = map[string]*BrowserTab{}
	s.mu.Unlock()
	if browser != nil {
		_ = browser.Close()
	}
	if launch != nil {
		launch.Cleanup()
	}
}

/* ── WS 支撑 ── */

/*
SnapshotFrame 返回标签最新帧(hello 恢复用);screencast 尚未出帧时补拍
一张,保证重连即有画面。
*/
func (s *BrowserService) SnapshotFrame(tabID string) (data []byte, width, height int, ok bool) {
	tab, ok := s.get(tabID)
	if !ok {
		return nil, 0, 0, false
	}
	if data, width, height = tab.snapshot(); data != nil {
		return data, width, height, true
	}
	shot, err := browserCapture(tab.hold(), false)
	if err != nil || len(shot) == 0 {
		return nil, 0, 0, false
	}
	shotWidth, shotHeight := jpegSize(shot)
	if shotWidth == 0 || shotHeight == 0 {
		return nil, 0, 0, false
	}
	return shot, shotWidth, shotHeight, true
}

/* Subscribe 订阅广播帧(WS 连接用);返回退订函数。 */
func (s *BrowserService) Subscribe() (<-chan BrowserFrame, func()) {
	ch := make(chan BrowserFrame, browserSubsBuf)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

func (s *BrowserService) broadcast(f BrowserFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- f:
		default: // 慢消费者丢帧,重连 hello 快照兜底
		}
	}
}

func (s *BrowserService) broadcastTabs() {
	s.broadcast(BrowserFrame{Type: "tabs", Tabs: s.List()})
}

/* browserKeyCode DOM 键名 → Windows 虚拟键码(组合键/控制键注入用)。 */
func browserKeyCode(key string) int {
	switch key {
	case "Enter":
		return 0x0D
	case "Backspace":
		return 0x08
	case "Tab":
		return 0x09
	case "Escape":
		return 0x1B
	case "Space":
		return 0x20
	case "Delete":
		return 0x2E
	case "Insert":
		return 0x2D
	case "Home":
		return 0x24
	case "End":
		return 0x23
	case "PageUp":
		return 0x21
	case "PageDown":
		return 0x22
	case "ArrowLeft":
		return 0x25
	case "ArrowUp":
		return 0x26
	case "ArrowRight":
		return 0x27
	case "ArrowDown":
		return 0x28
	}
	if num, ok := strings.CutPrefix(key, "F"); ok {
		if n, err := strconv.Atoi(num); err == nil && n >= 1 && n <= 12 {
			return 111 + n
		}
	}
	return 0
}
