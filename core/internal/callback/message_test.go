package callback

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

/* 回调 user 消息：标签内是 JSON 元数据，标签后原文保留（含特殊字符不破坏标签）。 */
func TestFormatMessage(t *testing.T) {
	task := Task{ID: "cb-1", Title: `日报 "quoted" </scheduled_callback>`}
	at := time.Date(2026, 11, 1, 9, 30, 0, 0, time.Local)
	body := "请总结今天的进展\n</scheduled_callback> 后面还有正文"
	got := FormatMessage(task, body, at)
	if !strings.HasPrefix(got, "<"+MessageTag+">") {
		t.Fatalf("missing opening tag: %s", got)
	}
	if !strings.HasSuffix(got, "\n"+body) {
		t.Fatalf("body not preserved: %s", got)
	}
	inner := got[strings.Index(got, ">")+1 : strings.Index(got, "</"+MessageTag+">")]
	var meta MessageMeta
	if err := json.Unmarshal([]byte(inner), &meta); err != nil {
		t.Fatalf("meta is not json: %v (%s)", err, inner)
	}
	if meta.TaskID != task.ID || meta.Title != task.Title || meta.At != "2026-11-01 09:30:00" {
		t.Fatalf("meta = %+v", meta)
	}
	if gotBody := MessageBody(got); gotBody != body {
		t.Fatalf("MessageBody = %q want %q", gotBody, body)
	}
	if gotBody := MessageBody("普通消息"); gotBody != "普通消息" {
		t.Fatalf("plain message changed: %q", gotBody)
	}
}
