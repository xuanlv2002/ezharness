/*
chat 单轮问答：附着 core → 静默登录 → 向当前活动分支发一句 → 只打印本轮
最终回复（工具/思考过程不输出），turn_end 后退出。参数为空且 stdin 是
管道时读整段输入；已有轮次运行中时跟随其输出到结束。
*/
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"
)

func cmdChat(rest []string, server string) {
	text := strings.TrimSpace(strings.Join(rest, " "))
	if text == "" && !isTerminal(os.Stdin) {
		data, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		text = strings.TrimSpace(string(data))
	}

	base := resolveBase(server)
	cl := ensureAuth(base)
	b, err := cl.bootstrap()
	if err != nil {
		fatalf("获取会话失败: %v", err)
	}
	if b.SessionID == "" {
		fatalf("没有活动会话（先在 tui 里 /new 开一条）")
	}
	if text == "" {
		fmt.Println(`用法：ezharness chat "一句话"（也可从管道读入）`)
		return
	}

	done := make(chan struct{})
	ended := false
	var mu sync.Mutex // 保护 stdout 输出交错
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
		case "model_end":
			var d modelEndData
			if json.Unmarshal(f.Data, &d) == nil && len(d.ToolCalls) == 0 && d.Content != "" {
				fmt.Print(d.Content)
			}
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

	if err := cl.send(b.SessionID, text, ""); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusConflict {
			// 已有轮次运行中：跟随现有输出到本轮结束
			fmt.Fprintf(os.Stderr, "[已有轮次运行中，跟随当前输出]\n")
		} else {
			fatalf("发送失败: %v", err)
		}
	}

	select {
	case <-done:
	case <-time.After(10 * time.Minute):
		fmt.Println("\n[超时] 本轮超过 10 分钟未结束")
	}
	fmt.Println()
}
