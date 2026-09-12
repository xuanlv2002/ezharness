/*
魔法看板·共享浏览器——core 侧桥。browser_* 工具的端无关定义在 core
(tools 层),真实浏览器是 desktop 壳的资产(Electron WebContentsView):
core 经 /api/browser/bridge WS 把工具调用(JSON-RPC 式)转发给 desktop
执行并等回执;截图由 desktop 回传 base64、core 落盘并走 image_loaded
通道转持久化图片消息。桥未连接(web 端直连、desktop 未跑)时工具报
"仅桌面端可用"。服务无页面状态,换代不重建;桥断线由 desktop 自动重连。
*/
package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/coder/websocket"
)

/* bridgeRequest 是 core → desktop 的调用帧。 */
type bridgeRequest struct {
	ID     int64          `json:"id"`
	Method string         `json:"method"` // start|navigate|click|type|key|scroll|read|screenshot|list|close
	Params map[string]any `json:"params"`
}

/* bridgeReply 是 desktop → core 的回执帧。 */
type bridgeReply struct {
	ID       int64  `json:"id"`
	OK       bool   `json:"ok"`
	Result   string `json:"result"` // 工具返回文本(含头行)
	Error    string `json:"error"`
	ImageB64 string `json:"imageB64,omitempty"` // screenshot 的 PNG base64
}

/* BrowserService 是浏览器桥(单连接:新 desktop 连接顶替旧连接)。 */
type BrowserService struct {
	mu        sync.Mutex
	writeMu   sync.Mutex // WS 单写者
	seq       int64
	conn      *websocket.Conn
	pending   map[int64]chan bridgeReply
	shotDir   string
	seesImages func() bool
}

/* NewBrowserService 构造(workDir 为 agent 工作区,截图落 workspace/browser)。 */
func NewBrowserService(workDir string) *BrowserService {
	return &BrowserService{
		pending: map[int64]chan bridgeReply{},
		shotDir: filepath.Join(workDir, "browser"),
	}
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
	s.mu.Lock()
	old := s.conn
	s.conn = conn
	s.mu.Unlock()
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
(导航类用 timeoutMs,其余 30s)。
*/
func (s *BrowserService) call(method string, params map[string]any, timeout time.Duration) (bridgeReply, error) {
	s.mu.Lock()
	if s.conn == nil {
		s.mu.Unlock()
		return bridgeReply{}, fmt.Errorf("浏览器不可用:需要 desktop 桌面端(web 端直连无内嵌浏览器)")
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
		wctx, wcancel := context.WithTimeout(context.Background(), 5*time.Second)
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

/* StartBrowser 新建标签并可选导航,返回创建头行与页面标题。 */
func (s *BrowserService) StartBrowser(desc, rawURL string, timeoutMs int) (string, error) {
	reply, err := s.call("start", map[string]any{
		"desc": desc, "url": rawURL,
		"timeoutMs": int(bridgeTimeout(timeoutMs, 20000, 600000).Milliseconds()),
	}, bridgeTimeout(timeoutMs, 25000, 600000))
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* NavigateBrowser 跳转并等加载,返回新页面标题。 */
func (s *BrowserService) NavigateBrowser(tabID, rawURL string, timeoutMs int) (string, error) {
	reply, err := s.call("navigate", map[string]any{
		"tabId": tabID, "url": rawURL,
		"timeoutMs": int(bridgeTimeout(timeoutMs, 20000, 120000).Milliseconds()),
	}, bridgeTimeout(timeoutMs, 25000, 120000))
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* ClickBrowser 点击:selector 优先,否则按视口坐标(desktop 换算注入)。 */
func (s *BrowserService) ClickBrowser(tabID, selector string, x, y int) (string, error) {
	reply, err := s.call("click", map[string]any{
		"tabId": tabID, "selector": selector, "x": x, "y": y,
	}, 30*time.Second)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* TypeBrowser 输入文本:selector 定位输入框(省略=当前焦点处),submit 回车提交。 */
func (s *BrowserService) TypeBrowser(tabID, selector, text string, submit bool) (string, error) {
	reply, err := s.call("type", map[string]any{
		"tabId": tabID, "selector": selector, "text": text, "submit": submit,
	}, 30*time.Second)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* PressBrowserKey 按键/组合键("Enter"、"Control+A")。 */
func (s *BrowserService) PressBrowserKey(tabID, combo string) (string, error) {
	reply, err := s.call("key", map[string]any{"tabId": tabID, "combo": combo}, 30*time.Second)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* ScrollBrowser 页面滚动(direction=up|down,amountPx 默认 600)。 */
func (s *BrowserService) ScrollBrowser(tabID, direction string, amountPx int) (string, error) {
	if amountPx <= 0 {
		amountPx = 600
	}
	reply, err := s.call("scroll", map[string]any{
		"tabId": tabID, "direction": direction, "amountPx": amountPx,
	}, 30*time.Second)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* ReadBrowser 读页面内容:mode=text 返回正文,mode=links 返回链接清单。 */
func (s *BrowserService) ReadBrowser(tabID, mode string, chars int) (string, error) {
	if chars <= 0 {
		chars = 4000
	}
	if chars > 20000 {
		chars = 20000
	}
	reply, err := s.call("read", map[string]any{
		"tabId": tabID, "mode": mode, "chars": chars,
	}, 30*time.Second)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/*
ScreenshotBrowser 截图:desktop 回 PNG base64,core 落盘 workspace/
browser/<tabID>.jpg(覆写)。主模型有视觉时返回 image_loaded 标记(由
filetools 转持久化图片消息),无视觉返回路径文字引导。
*/
func (s *BrowserService) ScreenshotBrowser(tabID string, fullPage bool) (string, error) {
	reply, err := s.call("screenshot", map[string]any{
		"tabId": tabID, "fullPage": fullPage,
	}, 30*time.Second)
	if err != nil {
		return "", err
	}
	if reply.ImageB64 == "" {
		return "", fmt.Errorf("截图失败:desktop 未回传图像数据")
	}
	png, err := base64.StdEncoding.DecodeString(reply.ImageB64)
	if err != nil || len(png) == 0 {
		return "", fmt.Errorf("截图数据解码失败")
	}
	if err := os.MkdirAll(s.shotDir, 0o755); err != nil {
		return "", fmt.Errorf("创建截图目录失败: %w", err)
	}
	path := filepath.Join(s.shotDir, tabID+".png")
	if err := os.WriteFile(path, png, 0o644); err != nil {
		return "", fmt.Errorf("写截图失败: %w", err)
	}
	head := fmt.Sprintf("[浏览器 #%s 截图已生成]", tabID)
	if !s.ModelSeesImages() {
		return head + "\n已保存至 " + path + ";当前模型无多模态能力,如需识别请在设置·模型启用视觉或图片识别槽", nil
	}
	return head + "\n" + `<image_loaded path="` + path + `"/>`, nil
}

/* CloseBrowserTab 关闭标签;幂等。 */
func (s *BrowserService) CloseBrowserTab(tabID string) error {
	_, err := s.call("close", map[string]any{"tabId": tabID}, 15*time.Second)
	return err
}

/* ListBrowserTabsJSON 标签清单 JSON 文本(browser_list 工具直接返回)。 */
func (s *BrowserService) ListBrowserTabsJSON() string {
	reply, err := s.call("list", nil, 15*time.Second)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return reply.Result
}
