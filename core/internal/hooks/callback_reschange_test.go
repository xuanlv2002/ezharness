package hooks

import (
	"slices"
	"testing"

	"ezharness/core/internal/callback"
)

/* 回调基线 diff：新增/更新/启停/移除各有明确文案，运行态不进签名。 */
func TestDiffCallbackSignatures(t *testing.T) {
	task := callback.Task{
		ID: "cb-1", Title: "每日总结", Kind: callback.KindApp, Enabled: true,
		Schedule: callback.Schedule{Kind: callback.ScheduleInterval, Every: "1h"},
		App:      &callback.AppTarget{Name: "probe", Callback: "sync"},
	}
	on := task.Signature()
	if got := diffCallbackSignatures(nil, []string{on}); !slices.Equal(got, []string{"新增定时回调 每日总结"}) {
		t.Fatalf("add = %v", got)
	}
	off := task
	off.Enabled = false
	if got := diffCallbackSignatures([]string{on}, []string{off.Signature()}); !slices.Equal(got, []string{"停用定时回调 每日总结"}) {
		t.Fatalf("disable = %v", got)
	}
	renamed := task
	renamed.Title = "每日总结 v2"
	if got := diffCallbackSignatures([]string{on}, []string{renamed.Signature()}); !slices.Equal(got, []string{"更新定时回调 每日总结 v2"}) {
		t.Fatalf("update = %v", got)
	}
	if got := diffCallbackSignatures([]string{on}, nil); !slices.Equal(got, []string{"移除定时回调 每日总结"}) {
		t.Fatalf("remove = %v", got)
	}
	// 运行态变化不产生资源变更（签名只取配置）
	ran := task
	ran.LastStatus = callback.StatusOK
	ran.NextRunAt = 123
	if got := diffCallbackSignatures([]string{on}, []string{ran.Signature()}); len(got) != 0 {
		t.Fatalf("runtime must not diff: %v", got)
	}
	// 脚本命令改动算配置更新（内容进签名哈希）
	script := callback.Task{
		ID: "cb-2", Title: "同步脚本", Kind: callback.KindScript, Enabled: true,
		Schedule: callback.Schedule{Kind: callback.ScheduleInterval, Every: "1h"},
		Script:   &callback.ScriptTarget{Command: "echo a"},
	}
	changed := script
	changed.Script = &callback.ScriptTarget{Command: "echo b"}
	if got := diffCallbackSignatures([]string{script.Signature()}, []string{changed.Signature()}); !slices.Equal(got, []string{"更新定时回调 同步脚本"}) {
		t.Fatalf("script update = %v", got)
	}
}
