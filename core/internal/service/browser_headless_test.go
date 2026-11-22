package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

/*
无头后端集成测试：本机无 Chrome/Edge 时跳过。覆盖开标签→读正文→eval→

	截图→控制台/异常→网络明细→关标签全链路（httptest 本地页面，不依赖外网）。
*/
func TestHeadlessBrowserFlow(t *testing.T) {
	if _, err := findBrowserBin(); err != nil {
		t.Skip("本机无可用浏览器，跳过：", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	h := newHeadlessBrowser()
	defer h.Close()

	html := `<html><head><title>冒烟页</title></head><body>
		<h1 id="t">你好 ezharness</h1>
		<button aria-label="确定">确定</button>
		<script>console.log('hello-console')</script>
	</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/data" {
			w.Header().Set("X-Test", "yes")
			fmt.Fprint(w, "hello-body")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	}))
	defer srv.Close()
	pageURL := srv.URL

	out, err := h.StartBrowser(ctx, "冒烟", "无头测试", pageURL, 15000)
	if err != nil {
		t.Fatal("StartBrowser:", err)
	}
	var st pageState
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("状态 JSON 解不开: %v (%s)", err, out)
	}
	if !strings.HasPrefix(st.ID, "b") {
		t.Fatalf("标签 id = %q", st.ID)
	}
	if st.Title != "冒烟页" {
		t.Fatalf("title = %q, want 冒烟页（状态：%s）", st.Title, out)
	}

	// read text
	readOut, err := h.ReadBrowser(ctx, st.ID, "text", "", 4000, 5000)
	if err != nil {
		t.Fatal("ReadBrowser:", err)
	}
	if !strings.Contains(readOut, "你好 ezharness") {
		t.Fatalf("正文缺失：%s", readOut)
	}

	// read elements（可交互元素含按钮与生成的 selector）
	elemOut, err := h.ReadBrowser(ctx, st.ID, "elements", "", 4000, 5000)
	if err != nil {
		t.Fatal("ReadBrowser(elements):", err)
	}
	if !strings.Contains(elemOut, "确定") {
		t.Fatalf("elements 缺按钮：%s", elemOut)
	}

	// eval：点击按钮（顶层 await + 返回值）
	evalOut, err := h.EvalBrowser(ctx, st.ID, `(async()=>{document.querySelector('#t').textContent='已点击';return document.title})()`)
	if err != nil {
		t.Fatal("EvalBrowser:", err)
	}
	if !strings.Contains(evalOut, "冒烟页") {
		t.Fatalf("eval 返回值缺失：%s", evalOut)
	}
	htmlOut, err := h.ReadBrowser(ctx, st.ID, "html", "#t", 4000, 5000)
	if err != nil {
		t.Fatal("ReadBrowser(html):", err)
	}
	if !strings.Contains(htmlOut, "已点击") {
		t.Fatalf("DOM 未更新：%s", htmlOut)
	}

	// 截图（视口 + 元素级）
	imgs, _, err := h.ScreenshotRaw(ctx, st.ID, false, "", 5000)
	if err != nil || len(imgs) != 1 || len(imgs[0]) < 100 {
		t.Fatalf("视口截图异常: err=%v n=%d", err, len(imgs))
	}
	imgs, note, err := h.ScreenshotRaw(ctx, st.ID, false, "#t", 5000)
	if err != nil || len(imgs) != 1 || !strings.Contains(note, "元素") {
		t.Fatalf("元素截图异常: err=%v note=%q", err, note)
	}

	// 控制台采集（页面 console.log 已捕获）
	consoleOut, err := h.ConsoleBrowser(ctx, st.ID, "read", false)
	if err != nil {
		t.Fatal("ConsoleBrowser:", err)
	}
	if !strings.Contains(consoleOut, "hello-console") {
		t.Fatalf("控制台未采集到：%s", consoleOut)
	}

	// 未捕获异常也进控制台（与桥的 devtools 采集对齐）
	if _, err := h.EvalBrowser(ctx, st.ID, `setTimeout(() => { throw new Error('boom-test') }, 0)`); err != nil {
		t.Fatal("EvalBrowser(throw):", err)
	}
	time.Sleep(300 * time.Millisecond)
	consoleOut, err = h.ConsoleBrowser(ctx, st.ID, "read", true)
	if err != nil || !strings.Contains(consoleOut, "boom-test") {
		t.Fatalf("未捕获异常未采集: err=%v out=%s", err, consoleOut)
	}

	// 网络明细（请求/响应头 + 响应体）
	if _, err := h.EvalBrowser(ctx, st.ID, `fetch('/data').then((r) => r.text())`); err != nil {
		t.Fatal("EvalBrowser(fetch):", err)
	}
	netIdx := 0
	for i := 0; i < 60 && netIdx == 0; i++ {
		time.Sleep(50 * time.Millisecond)
		tab, terr := h.tab(st.ID)
		if terr != nil {
			break
		}
		tab.mu.Lock()
		for j, key := range tab.netOrder {
			if strings.Contains(tab.nets[key].URL, "/data") {
				netIdx = j + 1
			}
		}
		tab.mu.Unlock()
	}
	if netIdx == 0 {
		t.Fatal("网络清单未采集到 /data")
	}
	detail, err := h.NetworkBrowser(ctx, st.ID, netIdx, "", false)
	if err != nil {
		t.Fatal("NetworkBrowser:", err)
	}
	for _, want := range []string{"请求头", "响应头", "X-Test", "响应体", "hello-body"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("网络明细缺 %q：%s", want, detail)
		}
	}

	// 清单与快照
	if list := h.ListTabsJSON(ctx); !strings.Contains(list, st.ID) {
		t.Fatalf("清单缺标签：%s", list)
	}
	if brief := h.TabsBrief(); len(brief) != 1 || !strings.Contains(brief[0], "冒烟") {
		t.Fatalf("TabsBrief = %v", brief)
	}

	// 关标签后不可再操作
	if _, err := h.CloseBrowserTab(ctx, st.ID); err != nil {
		t.Fatal("CloseBrowserTab:", err)
	}
	if _, err := h.ReadBrowser(ctx, st.ID, "text", "", 100, 1000); err == nil {
		t.Fatal("关闭后的标签仍可读")
	}
}
