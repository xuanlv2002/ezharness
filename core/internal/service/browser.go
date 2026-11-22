/*
魔法看板·共享浏览器——core 侧门面。browser_* 工具的端无关定义在 core
(tools 层)。两个后端按轮的来源端择一：桌面端发起的轮走桥（Electron
WebContentsView，真实浏览器 + 抽屉实时共见，经 /api/browser/bridge WS
转发并等回执）；web/cli/后台轮及桥未连接时用无头后端（chromedp 驱动本机
Chrome/Edge，见 browser_headless.go）——抽屉/共见是桌面端独有，能力面跨端
一致。截图统一 core 落盘并走 image_loaded 通道转持久化图片消息。服务无页面
状态,换代不重建;桥断线由 desktop 自动重连。
*/
package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

/* 桥调用等待上限分档：交互动作可等页面响应，快操作秒级，轮首快照不能拖住轮。 */
const (
	bridgeWriteTimeout = 5 * time.Second  // WS 帧发送
	bridgeOpTimeout    = 30 * time.Second // read/eval/screenshot 默认
	bridgeQuickTimeout = 15 * time.Second // close/list
	bridgeBriefTimeout = 2 * time.Second  // 轮首 TabsBrief 快照
)

/* bridgeRequest 是 core → desktop 的调用帧。 */
type bridgeRequest struct {
	ID     int64          `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

/* bridgeReply 是 desktop → core 的回执帧。 */
type bridgeReply struct {
	ID     int64    `json:"id"`
	OK     bool     `json:"ok"`
	Result string   `json:"result"`
	Error  string   `json:"error"`
	Images []string `json:"images,omitempty"` // screenshot 的分屏 PNG base64(整页分段)
	Note   string   `json:"note,omitempty"`   // 附加说明(如整页超出屏数上限)
}

/* BrowserService 是浏览器门面：按轮来源选后端（桥 / 无头），轮内不切——标签属于各自后端，中途切换会让 tabId 失效。 */
type BrowserService struct {
	mu         sync.Mutex
	writeMu    sync.Mutex // WS 单写者
	seq        int64
	conn       *websocket.Conn
	pending    map[int64]chan bridgeReply
	shotDir    string
	seesImages func() bool
	headless   *headlessBrowser
}

/* NewBrowserService 构造(workDir 为 agent 工作区,截图落 workspace/browser)。 */
func NewBrowserService(workDir string) *BrowserService {
	return &BrowserService{
		pending:  map[int64]chan bridgeReply{},
		shotDir:  filepath.Join(workDir, "browser"),
		headless: newHeadlessBrowser(),
	}
}

/* bridgeConnected 桥是否在连。 */
func (s *BrowserService) bridgeConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}

/* turnClientKey 把发起一轮的端（desktop/web/cli，空=后台）带进工具 ctx。 */
type turnClientKey struct{}

/* hd 无头后端：只有桌面端自己发起的一轮才走桥——桥是桌面壳的真实浏览器，桌面端在场时不该被 web/cli/后台轮劫持画面（也别在这些轮里悄悄开标签）。 */
func (s *BrowserService) hd(ctx context.Context) *headlessBrowser {
	if client, _ := ctx.Value(turnClientKey{}).(string); client == "desktop" && s.bridgeConnected() {
		return nil
	}
	return s.headless
}

/* Close 停无头浏览器并清 profile（core 退出/格式化时；桥由 desktop 侧断开）。 */
func (s *BrowserService) Close() {
	s.headless.Close()
}

/* SetVisionProbe 注入主模型视觉判定(Assemble 时调用,截图返回形态实时裁决)。 */
func (s *BrowserService) SetVisionProbe(fn func() bool) {
	s.mu.Lock()
	s.seesImages = fn
	s.mu.Unlock()
}

/* ModelSeesImages 主模型是否多模态(browser_screenshot 返回形态裁决)。 */
func (s *BrowserService) ModelSeesImages() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seesImages != nil && s.seesImages()
}

/* AttachBridge desktop 桥连入(顶替旧连接并唤醒其挂起调用);此后 pump 读回执。 */
func (s *BrowserService) AttachBridge(conn *websocket.Conn) {
	// 截图回执是整帧 base64(可达数 MB),默认 32KB 读限会读爆断桥
	conn.SetReadLimit(64 << 20)
	s.mu.Lock()
	old := s.conn
	s.conn = conn
	s.mu.Unlock()
	/* 桌面端接管后停掉无头后端：两套标签各用 b1/b2 编号，桥断开再重连时
	   留着无头标签会让同一个 id 指向不同页面。标签清空后模型按报错清单重建。 */
	s.headless.Close()
	if old != nil {
		_ = old.CloseNow() // 旧连接读泵退出,其 pending 由 detach 统一失败
	}
	s.pumpBridge(conn)
	s.mu.Lock()
	if s.conn == conn { // 只摘自己(可能已被更新连接顶替)
		s.conn = nil
	}
	// 挂起调用全部失败(桥已断,永无回执)
	for id, ch := range s.pending {
		close(ch)
		delete(s.pending, id)
	}
	s.mu.Unlock()
}

/* pumpBridge 读泵:回执按 id 分发到等待者。 */
func (s *BrowserService) pumpBridge(conn *websocket.Conn) {
	for {
		_, r, err := conn.Reader(context.Background())
		if err != nil {
			return
		}
		var reply bridgeReply
		if err := json.NewDecoder(r).Decode(&reply); err != nil {
			continue
		}
		s.mu.Lock()
		ch, ok := s.pending[reply.ID]
		delete(s.pending, reply.ID)
		s.mu.Unlock()
		if ok {
			ch <- reply
		}
	}
}

/*
call 转发一次工具调用并等回执。桥未连接立即报错;超时由调用方语义决定
(导航类用 timeoutMs,其余 30s);ctx 取消立即返回(用户停止本轮时打断在途
等待)。desktop 侧已在执行的操作不远程中断——回执迟到时 pumpBridge 查不到
pending 直接丢弃,在途的 capturePage/CDP 自然跑完。
*/
func (s *BrowserService) call(ctx context.Context, method string, params map[string]any, timeout time.Duration) (bridgeReply, error) {
	s.mu.Lock()
	if s.conn == nil {
		s.mu.Unlock()
		return bridgeReply{}, fmt.Errorf("浏览器桥已断开:重试即可(桥未连接时自动用无头浏览器)")
	}
	conn := s.conn
	s.seq++
	id := s.seq
	ch := make(chan bridgeReply, 1)
	s.pending[id] = ch
	s.mu.Unlock()

	payload, err := json.Marshal(bridgeRequest{ID: id, Method: method, Params: params})
	if err == nil {
		s.writeMu.Lock()
		wctx, wcancel := context.WithTimeout(context.Background(), bridgeWriteTimeout)
		err = conn.Write(wctx, websocket.MessageText, payload)
		wcancel()
		s.writeMu.Unlock()
	}
	if err != nil {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return bridgeReply{}, fmt.Errorf("浏览器桥发送失败: %w", err)
	}

	select {
	case reply, ok := <-ch:
		if !ok { // 桥断开,close 的 ch 返回零值
			return bridgeReply{}, fmt.Errorf("浏览器桥已断开")
		}
		if !reply.OK {
			return bridgeReply{}, fmt.Errorf("%s", reply.Error)
		}
		return reply, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return bridgeReply{}, fmt.Errorf("浏览器操作已取消: %w", ctx.Err())
	case <-time.After(timeout):
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return bridgeReply{}, fmt.Errorf("浏览器操作超时(%v)", timeout)
	}
}

func bridgeTimeout(timeoutMs, fallbackMs, maxMs int) time.Duration {
	if timeoutMs <= 0 {
		timeoutMs = fallbackMs
	}
	if timeoutMs > maxMs {
		timeoutMs = maxMs
	}
	return time.Duration(timeoutMs) * time.Millisecond
}

/* ── AI 工具后端(BrowserIO 实现,签名与 tools 层契约一致) ── */

/* StartBrowser 新建标签(name 必填,desc 可选)并可选导航,返回标签状态 JSON。 */
func (s *BrowserService) StartBrowser(ctx context.Context, name, desc, rawURL string, timeoutMs int) (string, error) {
	if h := s.hd(ctx); h != nil {
		return h.StartBrowser(ctx, name, desc, rawURL, timeoutMs)
	}
	reply, err := s.call(ctx, "start", map[string]any{
		"name": name, "desc": desc, "url": rawURL,
		"timeoutMs": int(bridgeTimeout(timeoutMs, 20000, 600000).Milliseconds()),
	}, bridgeTimeout(timeoutMs, 25000, 600000))
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* NavigateBrowser 跳转并等加载,返回新页面标题。 */
func (s *BrowserService) NavigateBrowser(ctx context.Context, tabID, rawURL string, timeoutMs int) (string, error) {
	if h := s.hd(ctx); h != nil {
		return h.NavigateBrowser(ctx, tabID, rawURL, timeoutMs)
	}
	reply, err := s.call(ctx, "navigate", map[string]any{
		"tabId": tabID, "url": rawURL,
		"timeoutMs": int(bridgeTimeout(timeoutMs, 20000, 120000).Milliseconds()),
	}, bridgeTimeout(timeoutMs, 25000, 120000))
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* UploadBrowser 给 <input type=file> 设置文件（files 为本机绝对路径,逗号分隔）。 */
func (s *BrowserService) UploadBrowser(ctx context.Context, tabID, selector, files string) (string, error) {
	if h := s.hd(ctx); h != nil {
		return h.UploadBrowser(ctx, tabID, selector, files)
	}
	reply, err := s.call(ctx, "upload", map[string]any{"tabId": tabID, "selector": selector, "files": files}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* SelectTab 把某个标签切到前台（用户也会看到它）。 */
func (s *BrowserService) SelectTab(ctx context.Context, tabID string) (string, error) {
	if h := s.hd(ctx); h != nil {
		return h.SelectTab(ctx, tabID)
	}
	reply, err := s.call(ctx, "activate", map[string]any{"tabId": tabID}, bridgeQuickTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/*
	ReadBrowser 读页面内容:mode=text 正文、html DOM、elements 可交互元素、
	links 链接、element 单元素(selector 定位)、frames 子帧清单(跨域帧够不着)。

timeoutMs 透传 desktop——页面仍在加载时先等收尾(默认 8000ms)再读,
免读到半页;桥等待上限随之放宽(超时+10s 余量给正文提取)。
*/
func (s *BrowserService) ReadBrowser(ctx context.Context, tabID, mode, selector string, chars, timeoutMs int) (string, error) {
	if h := s.hd(ctx); h != nil {
		return h.ReadBrowser(ctx, tabID, mode, selector, chars, timeoutMs)
	}
	if chars <= 0 {
		chars = 4000
	}
	if chars > 20000 {
		chars = 20000
	}
	timeout := bridgeOpTimeout
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs+10000) * time.Millisecond
	}
	reply, err := s.call(ctx, "read", map[string]any{
		"tabId": tabID, "mode": mode, "selector": selector, "chars": chars, "timeoutMs": timeoutMs,
	}, timeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* EvalBrowser 在页面内执行 JS,返回求值结果(可改 DOM、读状态)。 */
func (s *BrowserService) EvalBrowser(ctx context.Context, tabID, js string) (string, error) {
	if h := s.hd(ctx); h != nil {
		return h.EvalBrowser(ctx, tabID, js)
	}
	reply, err := s.call(ctx, "eval", map[string]any{"tabId": tabID, "js": js}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* ConsoleBrowser 读/清控制台(errorsOnly 只留 error;mode=read|clear)。 */
func (s *BrowserService) ConsoleBrowser(ctx context.Context, tabID, mode string, errorsOnly bool) (string, error) {
	if h := s.hd(ctx); h != nil {
		return h.ConsoleBrowser(ctx, tabID, mode, errorsOnly)
	}
	reply, err := s.call(ctx, "console", map[string]any{
		"tabId": tabID, "mode": mode, "errorsOnly": errorsOnly,
	}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* NetworkBrowser 读网络请求:index=0 清单,>0 该序号详情。 */
func (s *BrowserService) NetworkBrowser(ctx context.Context, tabID string, index int, filter string, errorsOnly bool) (string, error) {
	if h := s.hd(ctx); h != nil {
		return h.NetworkBrowser(ctx, tabID, index, filter, errorsOnly)
	}
	reply, err := s.call(ctx, "network", map[string]any{"tabId": tabID, "index": index, "filter": filter, "errorsOnly": errorsOnly}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/*
ScreenshotBrowser 截图:两个后端都回分屏 PNG base64(整页按视口高切片,每片
原生分辨率——一张大图会被视觉模型压缩到看不清),core 逐张落盘 workspace/
browser/<tabID>-<时间戳>-<序号>.png(每张一个新文件——图片按路径进上下文与
时间线,同名覆写会让历史截图全被最后一张顶掉)。主模型有视觉时每张给一个
image_loaded 标记(filetools 的 OnLoop 会把批内全部标记合并成一条多图 user
消息),无视觉返回路径文字引导。
*/
func (s *BrowserService) ScreenshotBrowser(ctx context.Context, tabID string, fullPage bool, selector, note string, timeoutMs int) (string, error) {
	images, shotNote := []string(nil), ""
	if h := s.hd(ctx); h != nil {
		imgs, n, err := h.ScreenshotRaw(ctx, tabID, fullPage, selector, timeoutMs)
		if err != nil {
			return "", err
		}
		images, shotNote = imgs, n
	} else {
		/* timeoutMs 透传:desktop 先等加载收尾(默认 10000ms)再截,消除
		"返回即截图"撞首帧未合成的竞态;桥上限 = 等待 + 截图余量 */
		timeout := bridgeOpTimeout
		if timeoutMs > 0 {
			timeout = time.Duration(timeoutMs+20000) * time.Millisecond
		}
		reply, err := s.call(ctx, "screenshot", map[string]any{
			"tabId": tabID, "fullPage": fullPage, "selector": selector, "timeoutMs": timeoutMs,
		}, timeout)
		if err != nil {
			return "", err
		}
		if len(reply.Images) == 0 {
			return "", fmt.Errorf("截图失败:desktop 未回传图像数据")
		}
		images, shotNote = reply.Images, reply.Note
	}
	return s.saveShots(tabID, images, shotNote, note)
}

/* saveShots 截图落盘与返回文本（两个后端共用）。 */
func (s *BrowserService) saveShots(tabID string, images []string, shotNote, note string) (string, error) {
	if err := os.MkdirAll(s.shotDir, 0o755); err != nil {
		return "", fmt.Errorf("创建截图目录失败: %w", err)
	}
	base := time.Now().UnixMilli()
	var paths []string
	for i, b64 := range images {
		png, derr := base64.StdEncoding.DecodeString(b64)
		if derr != nil || len(png) == 0 {
			continue
		}
		path := filepath.Join(s.shotDir, fmt.Sprintf("%s-%d-%d.png", tabID, base, i+1))
		if werr := os.WriteFile(path, png, 0o644); werr != nil {
			continue
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("截图数据解码失败")
	}
	head := fmt.Sprintf("[浏览器标签 %s 截图已生成(%d 张)]", tabID, len(paths))
	if shotNote != "" {
		head += "\n" + shotNote
	}
	if !s.ModelSeesImages() {
		return head + "\n已保存至 " + strings.Join(paths, "、") + ";当前模型无多模态能力,如需识别请在设置·模型为主模型勾选\"支持视觉\"", nil
	}
	note = imageNote(note)
	markers := make([]string, 0, len(paths))
	for _, p := range paths {
		if note != "" {
			markers = append(markers, `<image_loaded path="`+p+`" note="`+note+`"/>`)
			continue
		}
		markers = append(markers, `<image_loaded path="`+p+`"/>`)
	}
	return head + "\n" + strings.Join(markers, "\n"), nil
}

/*
	imageNote 把看图目的压成单行标记属性值(空白折叠、双引号换单引号,

属性正则才安全)。
*/
func imageNote(s string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(s), " "), `"`, `'`)
}

/* CloseBrowserTab 关闭标签;幂等,返回 desktop 回执的 closed JSON。 */
func (s *BrowserService) CloseBrowserTab(ctx context.Context, tabID string) (string, error) {
	if h := s.hd(ctx); h != nil {
		return h.CloseBrowserTab(ctx, tabID)
	}
	reply, err := s.call(ctx, "close", map[string]any{"tabId": tabID}, bridgeQuickTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* ListBrowserTabsJSON 标签清单 JSON 文本(browser_list 工具直接返回)。 */
func (s *BrowserService) ListBrowserTabsJSON(ctx context.Context) string {
	if h := s.hd(ctx); h != nil {
		return h.ListTabsJSON(ctx)
	}
	reply, err := s.call(ctx, "list", nil, bridgeQuickTimeout)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return reply.Result
}

/*
TabsBrief 返回开启中标签的展示名清单("name（页面标题）[tabId]",
agent_status 快照每轮现查用;id 供模型直接 browser_action/read 与
交付 browser:// 入口,免一次 list 发现)。清单与本轮工具的后端一致
(ctx 定桥/无头);查询失败/超时返回 nil(渲染为"（无）",不视为错误)。
自带 2 秒短超时:每轮 OnStart 同步调用,desktop 卡死不能拖住轮首。
*/
func (s *BrowserService) TabsBrief(ctx context.Context) []string {
	if h := s.hd(ctx); h != nil {
		return h.TabsBrief()
	}
	bctx, cancel := context.WithTimeout(context.Background(), bridgeBriefTimeout)
	defer cancel()
	reply, err := s.call(bctx, "list", nil, bridgeBriefTimeout)
	if err != nil {
		return nil
	}
	var tabs []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Desc  string `json:"desc"`
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	if json.Unmarshal([]byte(reply.Result), &tabs) != nil {
		return nil
	}
	out := make([]string, 0, len(tabs))
	for _, t := range tabs {
		label := t.Name
		seg := briefSeg(t.Title, 60)
		if seg == "" {
			seg = briefSeg(t.Desc, 60)
		}
		if seg == "" {
			seg = briefSeg(t.URL, 60)
		}
		if seg != "" {
			label += "（" + seg + "）"
		}
		out = append(out, label+"["+t.ID+"]")
	}
	return out
}
