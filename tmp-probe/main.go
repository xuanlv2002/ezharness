/*
一次性探针:复现两个用户反馈——镜像画面横向滚动条、打开大窗空白点不开。
流程:建标签(wikipedia,宽页)→ 模拟前端视口上报 → 存镜像帧 → 唤起真窗
→ Win32 GetWindowRect 验证窗口实际位置 → 收起 → 再验位置。
*/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/coder/websocket"
)

type outFrame struct {
	Type   string `json:"type"`
	TabID  string `json:"tabId"`
	Data   string `json:"data"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Tabs   []struct {
		ID string `json:"id"`
	} `json:"tabs"`
	Window *struct {
		Shown bool `json:"shown"`
	} `json:"window"`
}

func main() {
	base := "http://127.0.0.1:5260"
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := http.Post(base+"/api/browser/create", "application/json",
		strings.NewReader(`{"name":"probe","url":"zh.wikipedia.org"}`))
	if err != nil {
		fmt.Println("✗ 建标签失败:", err)
		return
	}
	raw := make([]byte, 200)
	n, _ := resp.Body.Read(raw)
	resp.Body.Close()
	fmt.Println("✓ 建标签:", string(raw[:n]))

	conn, _, err := websocket.Dial(ctx, "ws://127.0.0.1:5260/api/browser/ws", nil)
	if err != nil {
		fmt.Println("✗ WS 连接失败:", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(20 << 20)

	read := func() (map[string]any, error) {
		_, r, err := conn.Reader(ctx)
		if err != nil {
			return nil, err
		}
		var f map[string]any
		return f, json.NewDecoder(r).Decode(&f)
	}
	send := func(obj string) {
		wctx, wcancel := context.WithTimeout(ctx, 3*time.Second)
		defer wcancel()
		_ = conn.Write(wctx, websocket.MessageText, []byte(obj))
	}

	// 丢掉 hello
	_, err = read()
	if err != nil {
		fmt.Println("✗ hello 失败:", err)
		return
	}

	// 模拟前端视口跟随:860x640 @1.5(与真实抽屉接近)
	send(`{"type":"viewport","tabId":"b1","width":860,"height":640,"scaleFactor":1.5}`)

	// 等 re-layout 完成后再收帧(override 生效则帧应约 1290x960=860x640@1.5)
	var lastData string
	lastW, lastH := 0, 0
	frames := 0
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		f, err := read()
		if err != nil {
			fmt.Println("✗ 收帧失败:", err)
			return
		}
		if f["type"] == "frame" && f["tabId"] == "b1" {
			if d, ok := f["data"].(string); ok && len(d) > 2000 {
				lastData = d
				lastW = int(f["width"].(float64))
				lastH = int(f["height"].(float64))
				frames++
				if frames >= 2 { // 跳过 override 前的旧帧
					break
				}
			}
		}
	}
	if lastData == "" {
		fmt.Println("✗ 未收到镜像帧")
		return
	}
	fmt.Printf("✓ 镜像帧: %dx%d(视口 860x640@1.5 → 期望约 1290x960)\n", lastW, lastH)
	_ = os.WriteFile("b1_frame.b64", []byte(lastData), 0o644)
	fmt.Println("✓ base64 已存 b1_frame.b64(转码后查看)")

	// 唤起真窗口,验证窗口实际位置
	send(`{"type":"window","show":true,"tabId":"b1"}`)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f, err := read()
		if err != nil {
			break
		}
		if f["type"] == "window" {
			break
		}
	}
	fmt.Println("— 已发送 show=true,查询窗口位置:")
	rectCmd := `Add-Type -TypeDefinition 'using System;using System.Runtime.InteropServices;public class W{[DllImport("user32.dll")]public static extern bool GetWindowRect(IntPtr h,out R r);public struct R{public int L;public int T;public int Rr;public int B;}}'; $p = Get-Process chrome -ErrorAction SilentlyContinue | Where-Object { $_.Path -like '*rod*' -and $_.MainWindowHandle -ne 0 } | Select-Object -First 1; if ($p) { $r = New-Object W+R; [W]::GetWindowRect($p.MainWindowHandle, [ref]$r) | Out-Null; "rect: L=$($r.L) T=$($r.T) R=$($r.Rr) B=$($r.B)" } else { 'no-window' }`
	out, _ := execCombined(rectCmd)
	fmt.Println("  ", out)

	send(`{"type":"window","show":false,"tabId":"b1"}`)
	time.Sleep(1 * time.Second)
	fmt.Println("— 已发送 show=false,再查窗口位置:")
	out, _ = execCombined(rectCmd)
	fmt.Println("  ", out)
}

func execCombined(script string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
