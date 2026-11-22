/*
无头浏览器后端：桥（desktop 壳）未连接时，core 用 chromedp 驱动本机
Chrome/Edge 兜底——抽屉/实时共见是桌面端独有体验，能力面（导航/读页/
eval/截图/上传/控制台/网络）与桥对齐，标签状态 JSON 同构。浏览器二进制
不捆绑：探测失败的工具报错带安装指引（agent 可读，可自行安装或转告用户）。
*/
package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/chromedp/cdproto/dom"
	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

/* findBrowserBin 探测本机 Chrome/Chromium/Edge（按安装优先级），找不到给出 agent 可读的安装指引。 */
func findBrowserBin() (string, error) {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
		}
	case "windows":
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")} {
			if root == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
			)
		}
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("浏览器不可用:未找到本机 Chrome/Edge——请安装其一后重试(Windows: winget install Google.Chrome;macOS: brew install --cask google-chrome;Linux: 按发行版装 chromium),或启动 ezharness desktop 桌面端使用共享浏览器")
}

/* netEntry 是一条网络请求记录（network 工具读，明细带请求/响应头）。 */
type netEntry struct {
	Method     string
	URL        string
	Status     int64
	Type       string
	ReqHeaders map[string]any
	ResHeaders map[string]any
}

/*
headlessTab 是一个无头标签（同一浏览器上下文下的独立 target，cookie 与

	桌面端单一 partition 的语义一致：标签间共享登录态）。
*/
type headlessTab struct {
	id, name, desc string
	ctx            context.Context
	cancel         context.CancelFunc
	sem            chan struct{} // 命令串行（容量 1）
	mu             sync.Mutex
	console        []string
	nets           map[string]netEntry // requestID → 记录
	netOrder       []string
	lastTitle      string
	lastURL        string
}

/* headlessBrowser 管一个常驻无头浏览器实例（首个标签时启动）。 */
type headlessBrowser struct {
	mu       sync.Mutex
	started  bool
	bctx     context.Context
	bcancel  context.CancelFunc
	profile  string
	tabs     map[string]*headlessTab
	order    []string
	seq      int
	viewport [2]int64
}

func newHeadlessBrowser() *headlessBrowser {
	return &headlessBrowser{tabs: map[string]*headlessTab{}, viewport: [2]int64{1280, 900}}
}

/* ensure 首次使用时启动浏览器（探测 → 临时 profile → headless）。 */
func (h *headlessBrowser) ensure() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.started {
		return nil
	}
	bin, err := findBrowserBin()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "ez-browser-")
	if err != nil {
		return fmt.Errorf("创建浏览器 profile 失败: %w", err)
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(bin),
		chromedp.Flag("headless", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.UserDataDir(dir),
		chromedp.WindowSize(int(h.viewport[0]), int(h.viewport[1])),
	)
	h.bctx, h.bcancel = chromedp.NewExecAllocator(context.Background(), opts...)
	h.bctx, _ = chromedp.NewContext(h.bctx, chromedp.WithErrorf(func(format string, args ...any) {
		/* Chrome 新于 cdproto 的枚举值时（如 IPAddressSpace=Loopback）会刷
		   "could not unmarshal event"——事件跳过即可，不污染 core 日志 */
		if strings.Contains(format, "could not unmarshal event") {
			return
		}
		log.Printf(format, args...)
	}))
	/* 先跑一次让浏览器与默认标签落位：子上下文（每个标签一个 target）
	   只有在父上下文已持有浏览器时才能直接建新标签 */
	bdone := make(chan error, 1)
	go func() { bdone <- chromedp.Run(h.bctx) }()
	select {
	case err := <-bdone:
		if err != nil {
			h.bcancel()
			_ = os.RemoveAll(dir)
			return fmt.Errorf("浏览器启动失败: %w", err)
		}
	case <-time.After(20 * time.Second):
		h.bcancel()
		_ = os.RemoveAll(dir)
		return fmt.Errorf("浏览器 20 秒内未就绪（Chrome 启动失败？）")
	}
	h.profile = dir
	h.started = true
	return nil
}

/* Close 停浏览器并清 profile（core 退出/格式化时）。 */
func (h *headlessBrowser) Close() {
	h.mu.Lock()
	started, cancel, profile := h.started, h.bcancel, h.profile
	h.started, h.bcancel, h.profile = false, nil, ""
	h.tabs = map[string]*headlessTab{}
	h.order = nil
	h.mu.Unlock()
	if started && cancel != nil {
		cancel()
	}
	if profile != "" {
		_ = os.RemoveAll(profile)
	}
}

/* tab 取标签；不存在时报错附现有清单（模型自愈）。 */
func (h *headlessBrowser) tab(id string) (*headlessTab, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	t, ok := h.tabs[id]
	if !ok {
		var names []string
		for _, tid := range h.order {
			names = append(names, h.tabs[tid].id+"("+h.tabs[tid].name+")")
		}
		list := strings.Join(names, "、")
		if list == "" {
			list = "无"
		}
		return nil, fmt.Errorf("浏览器标签不存在（当前已有标签：%s）", list)
	}
	return t, nil
}

/* newTab 建标签：新 target + 控制台/网络采集。 */
func (h *headlessBrowser) newTab(name, desc string) *headlessTab {
	h.mu.Lock()
	h.seq++
	id := fmt.Sprintf("b%d", h.seq)
	parent := h.bctx
	h.mu.Unlock()
	tctx, cancel := chromedp.NewContext(parent)
	t := &headlessTab{id: id, name: name, desc: desc, ctx: tctx, cancel: cancel, sem: make(chan struct{}, 1), nets: map[string]netEntry{}}
	chromedp.ListenTarget(tctx, h.tabListener(t))
	h.mu.Lock()
	h.tabs[id] = t
	h.order = append(h.order, id)
	h.mu.Unlock()
	return t
}

/*
run 在标签上下文里执行 CDP 动作，命令按标签串行。chromedp 的坑：取消一个
"跑过 Run 的派生上下文"会让该标签会话失去响应（取消后响应分发失效，实测
v0.13.6/7 复现），所以超时/调用方取消只放弃等待、不取消底层运行；被放弃
的命令仍占着串行位，后续命令会以超时失败而不是挂死。
*/
func (h *headlessBrowser) run(ctx context.Context, t *headlessTab, timeout time.Duration, acts ...chromedp.Action) error {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case t.sem <- struct{}{}:
	case <-timer.C:
		return fmt.Errorf("浏览器操作超时(%v)", timeout)
	case <-ctx.Done():
		return ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		defer func() { <-t.sem }()
		done <- chromedp.Run(t.ctx, acts...)
	}()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return fmt.Errorf("浏览器操作超时(%v)", timeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}

/* tabListener 控制台/页面错误与网络事件采集（ring 上限：控制台 300 / 网络 400）。 */
func (h *headlessBrowser) tabListener(t *headlessTab) func(ev any) {
	pushConsole := func(level, text string) {
		line := time.Now().Format("15:04:05") + " " + level + " " + text
		t.mu.Lock()
		t.console = append(t.console, line)
		if len(t.console) > 300 {
			t.console = t.console[len(t.console)-300:]
		}
		t.mu.Unlock()
	}
	return func(ev any) {
		switch e := ev.(type) {
		case *cdpruntime.EventConsoleAPICalled:
			var parts []string
			for _, a := range e.Args {
				switch {
				case a.Value != nil:
					parts = append(parts, strings.TrimSpace(string(a.Value)))
				case a.Description != "":
					parts = append(parts, a.Description)
				case a.Type != "":
					parts = append(parts, string(a.Type))
				}
			}
			pushConsole(strings.ToUpper(string(e.Type)), strings.Join(parts, " "))
		case *cdpruntime.EventExceptionThrown:
			/* 未捕获异常（脚本报错/异步 rejection）：模型排查页面的第一手线索 */
			d := e.ExceptionDetails
			text := d.Text
			if d.Exception != nil && d.Exception.Description != "" {
				text = firstLine(d.Exception.Description)
			} else if d.Exception != nil && d.Exception.Value != nil {
				text = strings.TrimSpace(string(d.Exception.Value))
			}
			if d.URL != "" {
				text += fmt.Sprintf(" (%s:%d)", d.URL, int64(d.LineNumber)+1)
			}
			pushConsole("ERROR", "Uncaught "+text)
		case *cdplog.EventEntryAdded:
			en := e.Entry
			pushConsole(strings.ToUpper(string(en.Level)), en.Text)
		case *network.EventRequestWillBeSent:
			key := string(e.RequestID)
			t.mu.Lock()
			t.nets[key] = netEntry{Method: e.Request.Method, URL: e.Request.URL, Type: string(e.Type), ReqHeaders: e.Request.Headers}
			t.netOrder = append(t.netOrder, key)
			if len(t.netOrder) > 400 {
				delete(t.nets, t.netOrder[0])
				t.netOrder = t.netOrder[1:]
			}
			t.mu.Unlock()
		case *network.EventResponseReceived:
			key := string(e.RequestID)
			t.mu.Lock()
			if en, ok := t.nets[key]; ok {
				en.Status = e.Response.Status
				en.Type = string(e.Type)
				en.ResHeaders = e.Response.Headers
				t.nets[key] = en
			}
			t.mu.Unlock()
		}
	}
}

/* pageState 是标签状态 JSON（与桥同构；read/eval/console/network 的正文放 output）。 */
type pageState struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Desc    string `json:"desc"`
	Origin  string `json:"origin"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Loading bool   `json:"loading"`
	Note    string `json:"note,omitempty"`
	Output  string `json:"output,omitempty"`
}

/* evalAwaitPromise Evaluate 选项：顶层表达式是 Promise 时等其完成。 */
func evalAwaitPromise(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
	return p.WithAwaitPromise(true)
}

/*
evalRaw 页内执行 JS：returnByValue + awaitPromise，结果原文进 out
（字符串为 JSON 编码的字面量，对象为 JSON 文本；undefined 视为空）。
deepQuery 时先注入深查询前置。
*/
func (h *headlessBrowser) evalRaw(ctx context.Context, t *headlessTab, js string, out *string, deepQuery bool) error {
	expr := js
	if deepQuery {
		expr = deepQueryJS + "\n" + js
	}
	var raw []byte
	err := h.run(ctx, t, 30*time.Second, chromedp.Evaluate(expr, &raw, evalAwaitPromise))
	if err != nil {
		return fmt.Errorf("eval 执行出错：%s", firstLine(err.Error()))
	}
	if out == nil {
		return nil
	}
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" || s == "undefined" {
		*out = ""
		return nil
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		*out = str
		return nil
	}
	*out = s
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}

/* fetchState 现查页面 url/title/origin/readyState。 */
func (h *headlessBrowser) fetchState(ctx context.Context, t *headlessTab) pageState {
	st := pageState{ID: t.id, Name: t.name, Desc: t.desc}
	var raw string
	if err := h.evalRaw(ctx, t, `JSON.stringify({o:location.origin,u:location.href,t:document.title,l:document.readyState!=='complete'})`, &raw, false); err == nil && raw != "" {
		var v struct {
			O string `json:"o"`
			U string `json:"u"`
			T string `json:"t"`
			L bool   `json:"l"`
		}
		if json.Unmarshal([]byte(raw), &v) == nil {
			st.Origin, st.URL, st.Title, st.Loading = v.O, v.U, v.T, v.L
			t.mu.Lock()
			t.lastTitle, t.lastURL = v.T, v.U
			t.mu.Unlock()
		}
	}
	return st
}

/* stateJSON 状态 JSON 文本（note 可空）。 */
func (h *headlessBrowser) stateJSON(ctx context.Context, t *headlessTab, note string) string {
	st := h.fetchState(ctx, t)
	st.Note = note
	b, _ := json.Marshal(st)
	return string(b)
}

/* ── BrowserIO 操作面（签名与 tools 层契约一致） ── */

/* StartBrowser 新建标签并可选导航。 */
func (h *headlessBrowser) StartBrowser(ctx context.Context, name, desc, rawURL string, timeoutMs int) (string, error) {
	if err := h.ensure(); err != nil {
		return "", err
	}
	t := h.newTab(name, desc)
	if err := h.initTab(t); err != nil {
		h.dropTab(t.id)
		return "", err
	}
	note := "已创建(无头模式),后续操作用返回的 id;实时共见需 ezharness desktop"
	if rawURL == "" {
		return h.stateJSON(ctx, t, note), nil
	}
	if err := h.navigate(ctx, t, rawURL, timeoutMs); err != nil {
		return "", err
	}
	return h.stateJSON(ctx, t, note+";页面已加载"), nil
}

/* initTab 开域采集（console/network/log 需显式 enable）。 */
func (h *headlessBrowser) initTab(t *headlessTab) error {
	return h.run(context.Background(), t, 15*time.Second, cdpruntime.Enable(), network.Enable(), cdplog.Enable(), page.Enable())
}

/* navigate 等加载完成（失败不算错：DNS/拒绝照常返回状态）。 */
func (h *headlessBrowser) navigate(ctx context.Context, t *headlessTab, rawURL string, timeoutMs int) error {
	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	_ = h.run(ctx, t, timeout, chromedp.Navigate(completeURL(rawURL)))
	return nil
}

/* NavigateBrowser 跳转并回状态。 */
func (h *headlessBrowser) NavigateBrowser(ctx context.Context, tabID, rawURL string, timeoutMs int) (string, error) {
	t, err := h.tab(tabID)
	if err != nil {
		return "", err
	}
	if err := h.navigate(ctx, t, rawURL, timeoutMs); err != nil {
		return "", err
	}
	return h.stateJSON(ctx, t, ""), nil
}

/* UploadBrowser DOM.setFileInputFiles 塞文件（页面 JS 改不了 input.files）。 */
func (h *headlessBrowser) UploadBrowser(ctx context.Context, tabID, selector, files string) (string, error) {
	t, err := h.tab(tabID)
	if err != nil {
		return "", err
	}
	if selector == "" {
		return "", fmt.Errorf("upload 需要 selector")
	}
	var clean []string
	for _, f := range strings.Split(files, ",") {
		if f = strings.TrimSpace(f); f != "" {
			clean = append(clean, f)
		}
	}
	if len(clean) == 0 {
		return "", fmt.Errorf("upload 需要 files（本机绝对路径，逗号分隔）")
	}
	var multipleRaw string
	if err := h.evalRaw(ctx, t, `(()=>{const el=__ezOne(`+jsonString(selector)+`);return el?String(!!el.multiple):'missing'})()`, &multipleRaw, true); err != nil {
		return "", err
	}
	if multipleRaw == "missing" {
		return "", fmt.Errorf("元素 %s 未找到", selector)
	}
	apply := clean
	note := fmt.Sprintf("已向 %s 设置 %d 个文件", selector, len(apply))
	if multipleRaw != "true" {
		apply = clean[:1]
		if len(clean) > 1 {
			note += fmt.Sprintf("（该 input 非 multiple，只设置第一个，其余 %d 个被忽略）", len(clean)-1)
		}
	}
	var objID cdpruntime.RemoteObjectID
	err = h.run(ctx, t, 30*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
		obj, exp, err2 := cdpruntime.Evaluate(deepQueryJS + "\n__ezOne(" + jsonString(selector) + ")").Do(ctx)
		if err2 != nil {
			return err2
		}
		if exp != nil {
			return fmt.Errorf("%s", firstLine(exp.Error()))
		}
		if obj != nil {
			objID = obj.ObjectID
		}
		return nil
	}))
	if err != nil || objID == "" {
		return "", fmt.Errorf("定位 %s 失败", selector)
	}
	if err := h.run(ctx, t, 30*time.Second, dom.SetFileInputFiles(apply).WithObjectID(objID)); err != nil {
		return "", err
	}
	return h.stateJSON(ctx, t, note), nil
}

/* jsonString JSON 编码字符串常量（嵌入 JS 用）。 */
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

/* SelectTab 无头无真实前台，仅回状态。 */
func (h *headlessBrowser) SelectTab(ctx context.Context, tabID string) (string, error) {
	t, err := h.tab(tabID)
	if err != nil {
		return "", err
	}
	return h.stateJSON(ctx, t, "已激活"), nil
}

/* ReadBrowser 读页面：mode=text/html/elements/links/element/frames。 */
func (h *headlessBrowser) ReadBrowser(ctx context.Context, tabID, mode, selector string, chars, timeoutMs int) (string, error) {
	t, err := h.tab(tabID)
	if err != nil {
		return "", err
	}
	if chars <= 0 {
		chars = 4000
	}
	if chars > 20000 {
		chars = 20000
	}
	h.waitIdle(ctx, t, timeoutMs)

	var body string
	switch mode {
	case "elements":
		err = h.evalRaw(ctx, t, elementsJS, &body, true)
	case "html":
		if selector != "" {
			err = h.evalRaw(ctx, t, `(()=>{const el=__ezOne(`+jsonString(selector)+`);return el?el.outerHTML:'(未找到元素 '+`+jsonString(selector)+`+')'})()`, &body, true)
		} else {
			err = h.evalRaw(ctx, t, `document.documentElement.outerHTML`, &body, false)
		}
	case "element":
		if selector == "" {
			return "", fmt.Errorf("mode=element 需要 selector")
		}
		err = h.evalRaw(ctx, t, elementJS(selector), &body, true)
	case "links":
		err = h.evalRaw(ctx, t, linksJS, &body, true)
	case "frames":
		body = h.frameReport(ctx, t)
	default: // text
		err = h.evalRaw(ctx, t, `document.body?document.body.innerText:'(空页面)'`, &body, false)
	}
	if err != nil {
		return "", err
	}
	if mode == "elements" {
		if extra := h.frameReport(ctx, t); extra != "" {
			body += "\n" + extra
		}
	}
	body = strings.TrimSpace(body)
	note := ""
	if n := len([]rune(body)); n > chars {
		body = string([]rune(body)[:chars]) + "\n…(过长已截断)"
		note = "过长已截断"
	} else if body == "" {
		if mode == "links" {
			body = "(无链接)"
		} else {
			body = "(空页面)"
		}
	}
	st := h.fetchState(ctx, t)
	st.Note, st.Output = note, body
	b, _ := json.Marshal(st)
	return string(b), nil
}

/* waitIdle 等加载收尾（封顶放行，降级语义：晚到好过不到）。 */
func (h *headlessBrowser) waitIdle(ctx context.Context, t *headlessTab, timeoutMs int) {
	capMs := timeoutMs
	if capMs <= 0 {
		capMs = 8000
	}
	deadline := time.Now().Add(time.Duration(capMs) * time.Millisecond)
	for time.Now().Before(deadline) {
		if !h.fetchState(ctx, t).Loading {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}

/* frameReport 子 frame 清单（跨域帧读不到，提示模型直达 URL）。 */
func (h *headlessBrowser) frameReport(ctx context.Context, t *headlessTab) string {
	var tree *page.FrameTree
	err := h.run(ctx, t, 10*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
		ft, err2 := page.GetFrameTree().Do(ctx)
		tree = ft
		return err2
	}))
	if err != nil || tree == nil {
		return ""
	}
	var lines []string
	var walk func(n *page.FrameTree)
	walk = func(n *page.FrameTree) {
		for _, c := range n.ChildFrames {
			f := c.Frame
			name := ""
			if f.Name != "" {
				name = " [name=" + f.Name + "]"
			}
			url := f.URL
			if url == "" {
				url = "(空)"
			}
			lines = append(lines, "  "+url+name)
			walk(c)
		}
	}
	walk(tree)
	if len(lines) == 0 {
		return ""
	}
	return fmt.Sprintf("（页面含 %d 个子 frame；同源的已并入上面清单，跨域的读不到——可 browser_action navigate 直达其 URL 或改读主文档）\n%s", len(lines), strings.Join(lines, "\n"))
}

/* EvalBrowser 页内执行 JS（带深查询前置；顶层可 await）。 */
func (h *headlessBrowser) EvalBrowser(ctx context.Context, tabID, js string) (string, error) {
	t, err := h.tab(tabID)
	if err != nil {
		return "", err
	}
	h.waitIdle(ctx, t, 3000)
	var out string
	wrapped := "(async()=>{try{" + deepQueryJS + "\nreturn await (async()=>{" + js + "\n})()}catch(e){throw new Error((e&&(e.stack||e.message))||String(e))}})()"
	if err := h.evalRaw(ctx, t, wrapped, &out, false); err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		out = "(已执行，无返回值)"
	}
	if n := len([]rune(out)); n > 8000 {
		out = string([]rune(out)[:8000]) + "\n…(过长已截断)"
	}
	// 全局视觉变形检测（同桥：改 zoom/transform/filter 会污染坐标与截图）
	var deformed string
	_ = h.evalRaw(ctx, t, deformJS, &deformed, true)
	if deformed != "" {
		out += "\n⚠ 你改动了页面全局视觉属性（" + deformed + "）——这会持续偏移所有元素坐标、selector 截图会截到空白。还原后重试"
	}
	st := h.fetchState(ctx, t)
	st.Output = out
	b, _ := json.Marshal(st)
	return string(b), nil
}

/* ConsoleBrowser 读/清控制台。 */
func (h *headlessBrowser) ConsoleBrowser(ctx context.Context, tabID, mode string, errorsOnly bool) (string, error) {
	t, err := h.tab(tabID)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	st := pageState{ID: t.id, Name: t.name, Desc: t.desc, Title: t.lastTitle, URL: t.lastURL}
	if mode == "clear" {
		t.console = nil
		st.Output = "(控制台已清空)"
		b, _ := json.Marshal(st)
		return string(b), nil
	}
	var lines []string
	for _, l := range t.console {
		if errorsOnly && !strings.Contains(l, " ERROR ") {
			continue
		}
		lines = append(lines, l)
	}
	if len(lines) > 60 {
		lines = lines[len(lines)-60:]
	}
	st.Output = "(控制台为空)"
	if len(lines) > 0 {
		st.Output = strings.Join(lines, "\n")
	}
	b, _ := json.Marshal(st)
	return string(b), nil
}

/*
NetworkBrowser 读网络清单（最近 50 条；index>0 看单条：头 + 响应体）。

	响应体要跑 CDP，不能在持 t.mu 时调用（事件回调也要这把锁）。
*/
func (h *headlessBrowser) NetworkBrowser(ctx context.Context, tabID string, index int, filter string, errorsOnly bool) (string, error) {
	t, err := h.tab(tabID)
	if err != nil {
		return "", err
	}
	st := pageState{ID: t.id, Name: t.name, Desc: t.desc}
	t.mu.Lock()
	st.Title, st.URL = t.lastTitle, t.lastURL
	if index > 0 {
		if index > len(t.netOrder) {
			st.Output = fmt.Sprintf("(序号 %d 超出范围，共 %d 条)", index, len(t.netOrder))
			t.mu.Unlock()
			b, _ := json.Marshal(st)
			return string(b), nil
		}
		key := t.netOrder[index-1]
		en := t.nets[key]
		t.mu.Unlock()

		lines := []string{
			fmt.Sprintf("#%d %s %s → %s（%s）", index, en.Method, en.URL, statusText(en.Status), en.Type),
			"请求头:\n" + headerLines(en.ReqHeaders),
			"响应头:\n" + headerLines(en.ResHeaders),
		}
		if body := h.responseBody(ctx, t, key); body != "" {
			lines = append(lines, "响应体:\n"+body)
		}
		st.Output = strings.Join(lines, "\n")
		b, _ := json.Marshal(st)
		return string(b), nil
	}
	var lines []string
	for i := len(t.netOrder) - 1; i >= 0 && len(lines) < 50; i-- {
		en := t.nets[t.netOrder[i]]
		if filter != "" && !strings.Contains(en.URL, filter) {
			continue
		}
		if errorsOnly && (en.Status == 0 || en.Status < 400) {
			continue
		}
		lines = append(lines, fmt.Sprintf("%d. [%s %s] %s", i+1, en.Method, statusText(en.Status), en.URL))
	}
	t.mu.Unlock()
	st.Output = "(暂无请求记录)"
	if len(lines) > 0 {
		st.Output = strings.Join(lines, "\n")
	}
	b, _ := json.Marshal(st)
	return string(b), nil
}

/* headerLines 头清单文本（键排序，输出稳定）。 */
func headerLines(h map[string]any) string {
	if len(h) == 0 {
		return "  (无)"
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var lines []string
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("  %s: %v", k, h[k]))
	}
	return strings.Join(lines, "\n")
}

/* responseBody 取响应体（网络面板懒取；不可得时回一句提示，不影响头信息）。 */
func (h *headlessBrowser) responseBody(ctx context.Context, t *headlessTab, requestID string) string {
	var data []byte
	err := h.run(ctx, t, 8*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
		b, err2 := network.GetResponseBody(network.RequestID(requestID)).Do(ctx)
		data = b
		return err2
	}))
	if err != nil {
		return "(响应体不可用)"
	}
	if !utf8.Valid(data) {
		return "(二进制响应体，已省略)"
	}
	body := string(data)
	if body == "" {
		return "(空响应体)"
	}
	if len(body) > 4000 {
		body = body[:4000] + "\n…(响应体过长已截断)"
	}
	return body
}

func statusText(s int64) string {
	if s == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", s)
}

/*
ScreenshotRaw 截图（回 base64 分屏 + note；落盘与 image_loaded 标记在
BrowserService 门面统一做）：viewport 当前视口；fullPage 逐屏滚动（上限
5 段，与桥一致——大图会被视觉模型压到看不清）；selector 元素级截图。
*/
func (h *headlessBrowser) ScreenshotRaw(ctx context.Context, tabID string, fullPage bool, selector string, timeoutMs int) ([]string, string, error) {
	t, err := h.tab(tabID)
	if err != nil {
		return nil, "", err
	}
	h.waitIdle(ctx, t, timeoutMs)
	if selector != "" {
		return h.elementShot(ctx, t, selector)
	}
	if !fullPage {
		var buf []byte
		if err := h.run(ctx, t, 30*time.Second, chromedp.CaptureScreenshot(&buf)); err != nil {
			return nil, "", err
		}
		return []string{base64Encode(buf)}, "", nil
	}
	return h.fullShot(ctx, t)
}

/* elementShot 元素级截图（滚动到可见后按页面坐标裁剪）。 */
func (h *headlessBrowser) elementShot(ctx context.Context, t *headlessTab, selector string) ([]string, string, error) {
	var rect string
	if err := h.evalRaw(ctx, t, `(()=>{const el=__ezOne(`+jsonString(selector)+`);if(!el)return null;el.scrollIntoView({block:'center',inline:'center'});const b=__ezAbsRect(el);return JSON.stringify({x:b.left+window.scrollX,y:b.top+window.scrollY,w:b.width,h:b.height})})()`, &rect, true); err != nil {
		return nil, "", err
	}
	var r struct{ X, Y, W, H float64 }
	if rect == "" || json.Unmarshal([]byte(rect), &r) != nil {
		return nil, "", fmt.Errorf("元素 %s 未找到", selector)
	}
	w, ht := int(r.W+0.5), int(r.H+0.5)
	if w < 1 {
		w = 1
	}
	if ht < 1 {
		ht = 1
	}
	var data []byte
	err := h.run(ctx, t, 30*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
		d, err2 := page.CaptureScreenshot().
			WithCaptureBeyondViewport(true).
			WithClip(&page.Viewport{X: r.X, Y: r.Y, Width: float64(w), Height: float64(ht), Scale: 1}).
			Do(ctx)
		data = d
		return err2
	}))
	if err != nil {
		return nil, "", err
	}
	return []string{base64Encode(data)}, fmt.Sprintf("元素 %s，%d×%d", selector, w, ht), nil
}

/* fullShot 整页逐屏滚动截图（≤5 段，截完还原滚动位置）。 */
func (h *headlessBrowser) fullShot(ctx context.Context, t *headlessTab) ([]string, string, error) {
	var cssLV *page.LayoutViewport
	var cssCS *dom.Rect
	err := h.run(ctx, t, 15*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, _, clv, _, ccs, err2 := page.GetLayoutMetrics().Do(ctx)
		cssLV, cssCS = clv, ccs
		return err2
	}))
	if err != nil || cssLSMissing(cssLV, cssCS) {
		// 尺寸拿不到：退单张视口
		var buf []byte
		if err := h.run(ctx, t, 15*time.Second, chromedp.CaptureScreenshot(&buf)); err == nil {
			return []string{base64Encode(buf)}, "", nil
		}
		return nil, "", err
	}
	vh := cssLV.ClientHeight
	if vh < 200 {
		vh = 800
	}
	total := int64(cssCS.Height)
	segs := (total + vh - 1) / vh
	if segs < 1 {
		segs = 1
	}
	n := segs
	if n > 5 {
		n = 5
	}
	var pos string
	_ = h.evalRaw(ctx, t, `JSON.stringify([Math.round(window.scrollX),Math.round(window.scrollY)])`, &pos, false)
	var images []string
	for i := int64(0); i < n; i++ {
		_ = h.evalRaw(ctx, t, fmt.Sprintf(`window.scrollTo(0,%d)`, i*vh), nil, false)
		time.Sleep(120 * time.Millisecond) // 等这一屏落帧
		var buf []byte
		if err := h.run(ctx, t, 15*time.Second, chromedp.CaptureScreenshot(&buf)); err == nil && len(buf) > 0 {
			images = append(images, base64Encode(buf))
		}
	}
	if pos != "" {
		if xy := strings.Split(strings.Trim(pos, "[]"), ","); len(xy) == 2 {
			_ = h.evalRaw(ctx, t, fmt.Sprintf(`window.scrollTo(%s,%s)`, strings.TrimSpace(xy[0]), strings.TrimSpace(xy[1])), nil, false)
		}
	}
	if len(images) == 0 {
		return nil, "", fmt.Errorf("截图失败:页面长时间未合成首帧")
	}
	var notes []string
	if segs > n {
		notes = append(notes, fmt.Sprintf("页面共 %d 段，已截前 %d 段；其余滚动后逐屏看或 viewport 截图", segs, n))
	}
	if int64(cssCS.Width) > cssLV.ClientWidth+1 {
		notes = append(notes, fmt.Sprintf("页面有横向溢出（宽 %d，视口 %d），横向部分用 scroll left/right 看", int64(cssCS.Width), cssLV.ClientWidth))
	}
	return images, strings.Join(notes, "；"), nil
}

/* cssLSMissing 布局指标缺失判定。 */
func cssLSMissing(lv *page.LayoutViewport, cs *dom.Rect) bool {
	return lv == nil || cs == nil || cs.Height <= 0
}

/* dropTab 摘除标签并取消其上下文（关标签与初始化失败共用）。 */
func (h *headlessBrowser) dropTab(tabID string) {
	h.mu.Lock()
	t, ok := h.tabs[tabID]
	if ok {
		delete(h.tabs, tabID)
		for i, id := range h.order {
			if id == tabID {
				h.order = append(h.order[:i], h.order[i+1:]...)
				break
			}
		}
	}
	h.mu.Unlock()
	if ok {
		t.cancel()
	}
}

/* CloseBrowserTab 关标签（幂等）。 */
func (h *headlessBrowser) CloseBrowserTab(ctx context.Context, tabID string) (string, error) {
	h.dropTab(tabID)
	b, _ := json.Marshal(map[string]any{"id": tabID, "closed": true})
	return string(b), nil
}

/* ListTabsJSON 标签清单（未启动浏览器时为空清单——首个标签才拉起）。 */
func (h *headlessBrowser) ListTabsJSON(ctx context.Context) string {
	h.mu.Lock()
	started, order := h.started, append([]string(nil), h.order...)
	h.mu.Unlock()
	if !started {
		return "[]"
	}
	out := make([]pageState, 0, len(order))
	for _, id := range order {
		if t, err := h.tab(id); err == nil {
			out = append(out, h.fetchState(ctx, t))
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

/* TabsBrief 轮首快照条目（2s 内查完，拖不住轮首）。 */
func (h *headlessBrowser) TabsBrief() []string {
	h.mu.Lock()
	started := h.started
	order := append([]string(nil), h.order...)
	h.mu.Unlock()
	if !started || len(order) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), bridgeBriefTimeout)
	defer cancel()
	var out []string
	for _, id := range order {
		t, err := h.tab(id)
		if err != nil {
			continue
		}
		st := h.fetchState(ctx, t)
		label := st.Name
		seg := briefSeg(st.Title, 60)
		if seg == "" {
			seg = briefSeg(st.Desc, 60)
		}
		if seg == "" {
			seg = briefSeg(st.URL, 60)
		}
		if seg != "" {
			label += "（" + seg + "）"
		}
		out = append(out, label+"["+st.ID+"]")
	}
	return out
}

/* base64Encode png bytes → base64（截图回传统一格式）。 */
func base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

/* completeURL 补协议（裸域名按 https；localhost / 带端口按 http）。 */
func completeURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "about:blank"
	}
	for _, p := range []string{"http://", "https://", "file://", "about:", "data:"} {
		if strings.HasPrefix(u, p) {
			return u
		}
	}
	if strings.HasPrefix(u, "localhost") || strings.HasPrefix(u, "127.0.0.1") {
		return "http://" + u
	}
	return "https://" + u
}
