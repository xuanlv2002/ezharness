/*
chat 一问一答：附着 core → 登录 → 向当前活动分支发一句 → 流式打印回答
（工具调用只显示一行动作名），turn_end 后退出。已有轮次运行中时不再发送，
跟随当前输出到本轮结束。
*/
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"
)

func cmdChat(rest []string, server string) {
	text := strings.TrimSpace(strings.Join(rest, " "))
	base := resolveBase(server)
	cl := ensureAuth(base, server == "")
	b, err := cl.bootstrap()
	if err != nil {
		fatalf("获取会话失败: %v", err)
	}
	if b.SessionID == "" {
		fatalf("没有活动会话（先在桌面端或 web 端创建）")
	}

	done := make(chan struct{})
	ended := false
	var mu sync.Mutex // 保护 stdout 输出交错
	sawChunk := false // 模型分段已流式打印时，model_end 不再整段重打
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt)
	go func() {
		<-interrupted
		_ = cl.callJSON("POST", "/api/sessions/"+b.SessionID+"/cancel", nil, nil)
	}()

	stop := cl.sseStream(b.SessionID, func(f frame) {
		if f.ForkID != "" {
			return // 后台分身的帧不打印，也不结束本命令
		}
		mu.Lock()
		defer mu.Unlock()
		switch f.Type {
		case "tool_start":
			var d toolStartData
			if json.Unmarshal(f.Data, &d) == nil {
				fmt.Printf("▸ %s\n", d.Name)
			}
		case "model_chunk":
			var s string
			if json.Unmarshal(f.Data, &s) == nil {
				fmt.Print(s)
				sawChunk = true
			}
		case "model_end":
			var d modelEndData
			if json.Unmarshal(f.Data, &d) == nil && d.Content != "" && !sawChunk {
				fmt.Print(d.Content)
			}
			sawChunk = false
		case "turn_end":
			var d turnEndData
			_ = json.Unmarshal(f.Data, &d)
			if d.Err != "" {
				fmt.Printf("\n[出错] %s\n", d.Err)
			}
			if !ended {
				ended = true
				close(done)
			}
		}
	}, func(err error) {
		mu.Lock()
		fmt.Fprintf(os.Stderr, "\n[SSE 断开] %v\n", err)
		mu.Unlock()
	})
	defer stop()

	if text != "" {
		if err := cl.send(b.SessionID, text); err != nil {
			var ae *apiError
			if ok := errors.As(err, &ae); ok && ae.Status == http.StatusConflict {
				// 已有轮次运行中：跟随现有输出到本轮结束
				fmt.Fprintf(os.Stderr, "[已有轮次运行中，跟随当前输出]\n")
			} else {
				fatalf("发送失败: %v", err)
			}
		}
	} else {
		fmt.Println("[提示] chat 带一句话参数；无参时跟随当前分支输出（Ctrl+C 取消本轮）")
	}

	select {
	case <-done:
	case <-time.After(10 * time.Minute):
		fmt.Println("\n[超时] 本轮超过 10 分钟未结束")
	}
	fmt.Println()
}
