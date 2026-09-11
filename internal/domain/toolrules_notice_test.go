package domain

import (
	"encoding/json"
	"testing"
)

/*
	PendingNotices 只导出未决人机请求（含 fork 标识），其他 pending 类型与

坏帧剔除；通知栏全局轮询的数据源契约。
*/
func TestPendingNotices(t *testing.T) {
	s := &Session{pending: map[string]Event{
		"c1": {Type: "approve.request", Ts: 1000, Data: raw(ToolStartData{ID: "c1", Name: "terminal", Args: json.RawMessage(`{"cmd":"ls"}`)})},
		"c2": {Type: "askuser.request", ForkID: "f1", Ts: 2000, Data: raw(ToolStartData{ID: "c2", Name: "ask_user", Args: json.RawMessage(`{"question":"选哪个"}`)})},
		"c3": {Type: "model_chunk", Data: json.RawMessage(`"text"`)},
		"c4": {Type: "approve.request", Data: json.RawMessage(`{"id":""}`)},
	}}
	got := s.PendingNotices()
	if len(got) != 2 {
		t.Fatalf("want 2 notices, got %d: %+v", len(got), got)
	}
	byID := map[string]PendingNotice{}
	for _, n := range got {
		byID[n.CallID] = n
	}
	if n := byID["c1"]; n.Kind != "approve" || n.Tool != "terminal" || n.ForkID != "" || n.Ts != 1000 {
		t.Errorf("notice c1 mismatch: %+v", n)
	}
	if n := byID["c2"]; n.Kind != "ask" || n.ForkID != "f1" || n.Args != `{"question":"选哪个"}` {
		t.Errorf("notice c2 mismatch: %+v", n)
	}
}
