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

/* 工具/思考折叠：工具只计数，带工具调用的中间发言不落时间线，只显示最终回复。 */
func TestApplyFrameFoldsProcess(t *testing.T) {
	m := &tuiModel{}
	m.applyFrame(frame{Type: "tool_start", Data: json.RawMessage(`{"id":"c1","name":"terminal"}`)})
	if m.tools != 1 || len(m.lines) != 0 {
		t.Fatalf("tool_start 不该进时间线：tools=%d lines=%+v", m.tools, m.lines)
	}
	m.applyFrame(frame{Type: "model_end", Data: json.RawMessage(`{"content":"中间话","toolCalls":[{"id":"c1","name":"terminal"}]}`)})
	if len(m.lines) != 0 {
		t.Fatalf("带工具调用的中间发言应折叠：%+v", m.lines)
	}
	m.applyFrame(frame{Type: "model_end", Data: json.RawMessage(`{"content":"最终回答"}`)})
	if len(m.lines) != 1 || m.lines[0].kind != "assistant" || m.lines[0].text != "最终回答" {
		t.Fatalf("最终回复未显示：%+v", m.lines)
	}
}

/* 决策帧：提问/审批/计划都登记为待办并进时间线；重放重复帧按 id 去重。 */
func TestApplyFrameDecisions(t *testing.T) {
	m := &tuiModel{}
	m.applyFrame(frame{Type: "askuser.request", Data: json.RawMessage(`{"id":"c1","name":"ask_user","args":"{\"question\":\"选哪个\",\"options\":[\"A\",\"B\"]}"}`)})
	if len(m.pending) != 1 || m.pending[0].kind != "ask" || m.pending[0].question != "选哪个" || len(m.pending[0].options) != 2 {
		t.Fatalf("ask 解析失败：%+v", m.pending)
	}
	m.applyFrame(frame{Type: "askuser.request", Data: json.RawMessage(`{"id":"c1","name":"ask_user","args":"{}"}`)})
	if len(m.pending) != 1 {
		t.Fatalf("重复决策帧未去重：%+v", m.pending)
	}
	m.applyFrame(frame{Type: "approve.request", Data: json.RawMessage(`{"id":"c2","name":"terminal","args":{"cmd":"ls"}}`)})
	if len(m.pending) != 2 || m.pending[1].kind != "approve" || m.pending[1].name != "terminal" {
		t.Fatalf("approve 解析失败：%+v", m.pending)
	}
	m.applyFrame(frame{Type: "plan.request", Data: json.RawMessage(`{"id":"c3","name":"plan","args":{"goal":"做X","progress":[{"title":"a"},{"title":"b"}]}}`)})
	if len(m.pending) != 3 || m.pending[2].kind != "plan" || m.pending[2].goal != "做X" || m.pending[2].steps != 2 {
		t.Fatalf("plan 解析失败：%+v", m.pending)
	}
}
