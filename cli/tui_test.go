package main

import (
	"encoding/json"
	"testing"
)

// 本地回显的用户消息与 loop_start 回放同一条时只显示一次；回放重建
// （replay.sync）后清掉标记，让回放自己补上用户行。
func TestApplyFrameUserEcho(t *testing.T) {
	m := &tuiModel{}
	m.lines = []line{{kind: "user", text: "旧"}}
	m.histCount = len(m.lines)

	m.echoUser = "你好"
	m.applyFrame(frame{Type: "loop_start", Data: json.RawMessage(`"你好"`)})
	if len(m.lines) != 1 {
		t.Fatalf("重复回显：%+v", m.lines)
	}
	if m.echoUser != "" {
		t.Fatal("echo 标记未清")
	}

	// 带引用/附件的 loop_start 载荷是对象
	m.applyFrame(frame{Type: "loop_start", Data: json.RawMessage(`{"text":"带附件"}`)})
	if len(m.lines) != 2 || m.lines[1].text != "带附件" {
		t.Fatalf("对象载荷未解析：%+v", m.lines)
	}

	// 运行中切回分支：截回历史行，回放会重发 loop_start
	m.lines = append(m.lines, line{kind: "user", text: "回显"})
	m.echoUser = "回显"
	m.applyFrame(frame{Type: "replay.sync", Data: json.RawMessage(`{"turnActive":true}`)})
	if len(m.lines) != m.histCount || m.echoUser != "" {
		t.Fatalf("replay.sync 未截断/清标记：lines=%d hist=%d echo=%q", len(m.lines), m.histCount, m.echoUser)
	}
}

/* SSE 错误经帧通道回主循环（不直接改模型字段），并解除"运行中"阻塞。 */
func TestApplyFrameSseError(t *testing.T) {
	m := &tuiModel{streaming: true}
	m.applyFrame(frame{Type: "sse.error", Data: json.RawMessage(`"SSE 断开: boom"`)})
	if m.err != "SSE 断开: boom" || m.streaming {
		t.Fatalf("err=%q streaming=%v", m.err, m.streaming)
	}
}
