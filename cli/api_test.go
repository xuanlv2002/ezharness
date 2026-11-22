package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// SSE 订阅收到帧、stop 可重复调用且真正断开（不能 panic，服务端要看到
// 连接关闭）——对应早期的 done 通道双重 close 与 stop 不取消请求两个 bug。
func TestSSEStreamStop(t *testing.T) {
	closed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("EzHarnessToken"); got != "tok" {
			t.Errorf("token header = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"turn_end\",\"data\":null}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	defer srv.Close()

	c := &client{base: srv.URL, token: "tok"}
	frames := make(chan frame, 4)
	stop := c.sseStream("root", func(f frame) {
		frames <- f
	}, func(err error) {
		t.Errorf("unexpected stream error: %v", err)
	})

	select {
	case f := <-frames:
		if f.Type != "turn_end" {
			t.Fatalf("frame type = %q", f.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SSE 帧未送达")
	}

	stop()
	stop() // 幂等：重复 stop 不得 panic
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("stop 后服务端连接未断开")
	}
}
