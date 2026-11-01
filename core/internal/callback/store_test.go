package callback

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ezharness/core/internal/osfs"
)

/* store 生命周期：创建落盘、重新加载、更新、删除。 */
func TestStoreLifecycle(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx := context.Background()
	fsys := osfs.OS{}

	st, err := Load(ctx, fsys)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.List(); len(got) != 0 {
		t.Fatalf("new store = %+v", got)
	}
	created, err := st.Create(ctx, Task{
		Title: "每日总结", Kind: KindSession, Enabled: true,
		Schedule: Schedule{Kind: ScheduleInterval, Every: "24h"},
		Session:  &SessionTarget{SessionID: "s1", Mode: ModeSession, Message: "总结今天"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.CreatedAt == 0 || created.UpdatedAt == 0 {
		t.Fatalf("create defaults: %+v", created)
	}

	reloaded, err := Load(ctx, fsys)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(created.ID)
	if !ok || got.Title != "每日总结" || got.Session == nil || got.Session.Message != "总结今天" {
		t.Fatalf("reload = %+v ok=%v", got, ok)
	}
	tasks := LoadTasks(ctx, fsys)
	if len(tasks) != 1 || tasks[0].ID != created.ID {
		t.Fatalf("LoadTasks = %+v", tasks)
	}

	updated, err := reloaded.Update(ctx, created.ID, func(t *Task) error {
		t.Title = "每日总结 v2"
		return nil
	})
	if err != nil || updated.Title != "每日总结 v2" {
		t.Fatalf("update = %+v %v", updated, err)
	}
	// 校验失败不留半更新（错误返回后内存态仍是旧值）
	if _, err := reloaded.Update(ctx, created.ID, func(t *Task) error {
		t.Title = "坏标题"
		return errors.New("invalid")
	}); err == nil {
		t.Fatal("invalid update must fail")
	}
	if got, _ := reloaded.Get(created.ID); got.Title != "每日总结 v2" {
		t.Fatalf("failed update leaked: %+v", got)
	}
	if err := reloaded.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(created.ID); ok {
		t.Fatal("deleted task still present")
	}
	if err := reloaded.Delete(ctx, created.ID); err != ErrNotFound {
		t.Fatalf("delete missing = %v", err)
	}
}

/* 启动校正：中断的 running 归 skipped、错过的 once 转终态、间隔从 now 重算、停用清下次。 */
func TestStoreNormalize(t *testing.T) {
	t.Chdir(t.TempDir())
	now := time.Date(2026, 11, 1, 8, 30, 0, 0, time.Local)
	st := New(osfs.OS{})
	st.tasks = []Task{
		{
			ID: "run", Title: "running", Kind: KindApp, Enabled: true,
			Schedule:   Schedule{Kind: ScheduleInterval, Every: "1h"},
			App:        &AppTarget{Name: "a", Callback: "run"},
			LastStatus: StatusRunning, NextRunAt: now.Add(-time.Hour).UnixMilli(),
		},
		{
			ID: "once", Title: "once", Kind: KindApp, Enabled: true,
			Schedule:  Schedule{Kind: ScheduleOnce, At: "2026-01-01 09:00"},
			App:       &AppTarget{Name: "a", Callback: "run"},
			NextRunAt: now.Add(-time.Hour).UnixMilli(),
		},
		{
			ID: "int", Title: "int", Kind: KindApp, Enabled: true,
			Schedule:  Schedule{Kind: ScheduleInterval, Every: "1h"},
			App:       &AppTarget{Name: "a", Callback: "run"},
			NextRunAt: now.Add(-time.Minute).UnixMilli(),
		},
		{
			ID: "off", Title: "off", Kind: KindApp, Enabled: false,
			Schedule:  Schedule{Kind: ScheduleInterval, Every: "1h"},
			App:       &AppTarget{Name: "a", Callback: "run"},
			NextRunAt: 123,
		},
		{
			// 停用的一次性任务时间已过：永远无法再启用，启动即转终态
			ID: "offonce", Title: "off once", Kind: KindApp, Enabled: false,
			Schedule: Schedule{Kind: ScheduleOnce, At: "2026-01-01 09:00"},
			App:      &AppTarget{Name: "a", Callback: "run"},
		},
		{
			// 停用的一次性任务时间在未来：保留（用户可能重新启用）
			ID: "offfuture", Title: "off future", Kind: KindApp, Enabled: false,
			Schedule: Schedule{Kind: ScheduleOnce, At: "2099-01-01 09:00"},
			App:      &AppTarget{Name: "a", Callback: "run"},
		},
	}
	if err := st.Normalize(now); err != nil {
		t.Fatal(err)
	}
	byID := map[string]Task{}
	for _, task := range st.List() {
		byID[task.ID] = task
	}
	if byID["run"].LastStatus != StatusSkipped {
		t.Fatalf("running -> %q", byID["run"].LastStatus)
	}
	if byID["run"].NextRunAt <= now.UnixMilli() {
		t.Fatalf("interval next not recomputed: %+v", byID["run"])
	}
	if byID["once"].Enabled || !byID["once"].Finished() ||
		byID["once"].LastStatus != StatusMissed || byID["once"].NextRunAt != 0 {
		t.Fatalf("once missed: %+v", byID["once"])
	}
	if !byID["offonce"].Finished() {
		t.Fatalf("past disabled once not finished: %+v", byID["offonce"])
	}
	if byID["offfuture"].Finished() {
		t.Fatalf("future disabled once must stay open: %+v", byID["offfuture"])
	}
	if byID["int"].NextRunAt <= now.UnixMilli() {
		t.Fatalf("interval next not recomputed: %+v", byID["int"])
	}
	if byID["off"].NextRunAt != 0 {
		t.Fatalf("disabled next not cleared: %+v", byID["off"])
	}
}

/* SortTasks：启用中置顶，组内创建时间新→旧。 */
func TestSortTasks(t *testing.T) {
	tasks := []Task{
		{ID: "old-off", Enabled: false, CreatedAt: 100},
		{ID: "new-en", Enabled: true, CreatedAt: 300},
		{ID: "old-en", Enabled: true, CreatedAt: 200},
		{ID: "new-off", Enabled: false, CreatedAt: 400},
	}
	SortTasks(tasks)
	got := make([]string, len(tasks))
	for i, t := range tasks {
		got[i] = t.ID
	}
	want := []string{"new-en", "old-en", "new-off", "old-off"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v want %v", got, want)
	}
}
