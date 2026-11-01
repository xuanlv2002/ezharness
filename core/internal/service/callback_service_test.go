package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/callback"
	"ezharness/core/internal/hooks"
	"ezharness/core/internal/osfs"
)

/*
会话目标存具体叶 session ID：省略 sessionId 时绑定当前会话；状态卡只
显示绑定到当前会话的任务。
*/
func TestCallbackBindsCurrentSession(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	hub := newTestHub()
	fsys := osfs.OS{}
	root := "r-active"
	sessionID := "s-active"
	snap := &hooks.SessionSnap{
		ID: sessionID, LineRoot: root, SeedKind: "new",
		Messages: []types.Message{{Role: types.RoleUser, Content: "已有消息"}},
	}
	if err := hooks.SaveSnap(ctx, fsys, snap); err != nil {
		t.Fatal(err)
	}
	if err := hub.Topics.UpdateLeaf(root, sessionID, "已有消息", time.Now().UnixMilli(), 1); err != nil {
		t.Fatal(err)
	}
	active := hub.LoadBranch(root, snap)
	hub.SetActive(active)
	svc := &CallbackService{
		Hub: hub, Topics: &TopicService{Hub: hub, Agents: &AgentService{Hub: hub}},
		Store: callback.New(fsys), now: time.Now,
	}
	task, err := svc.Create(ctx, callback.CreateInput{
		Title:    "当前会话",
		Kind:     callback.KindSession,
		Schedule: callback.Schedule{Kind: callback.ScheduleInterval, Every: "24h"},
		Session:  &callback.SessionInput{Mode: callback.ModeSession, Message: "继续"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Session == nil || task.Session.SessionID != sessionID {
		t.Fatalf("default session = %+v", task.Session)
	}
	if task.NextRunAt <= time.Now().UnixMilli() {
		t.Fatalf("next run not set: %+v", task)
	}
	brief := svc.Brief(sessionID)
	if len(brief) != 1 || brief[0].ID != task.ID {
		t.Fatalf("brief = %+v", brief)
	}
	if got := svc.Brief(root); len(got) != 0 {
		t.Fatalf("branch root must not match direct session: %+v", got)
	}
}

/* 页面可选任意在线会话：显式 sessionId 优先于调用方当前会话，更新时也能改绑。 */
func TestCallbackBindsExplicitSession(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	hub := newTestHub()
	fsys := osfs.OS{}
	seed := func(root, id, title string) *hooks.SessionSnap {
		t.Helper()
		snap := &hooks.SessionSnap{
			ID: id, LineRoot: root, SeedKind: "new",
			Messages: []types.Message{{Role: types.RoleUser, Content: title}},
		}
		if err := hooks.SaveSnap(ctx, fsys, snap); err != nil {
			t.Fatal(err)
		}
		if err := hub.Topics.UpdateLeaf(root, id, title, time.Now().UnixMilli(), 1); err != nil {
			t.Fatal(err)
		}
		return snap
	}
	one := seed("r-one", "s-one", "会话一")
	two := seed("r-two", "s-two", "会话二")
	hub.SetActive(hub.LoadBranch("r-one", one))
	hub.LoadBranch("r-two", two)

	svc := &CallbackService{
		Hub: hub, Topics: &TopicService{Hub: hub, Agents: &AgentService{Hub: hub}},
		Store: callback.New(fsys), now: time.Now,
	}
	task, err := svc.Create(ctx, callback.CreateInput{
		Title:    "指定会话",
		Kind:     callback.KindSession,
		Schedule: callback.Schedule{Kind: callback.ScheduleInterval, Every: "1h"},
		Session:  &callback.SessionInput{SessionID: "s-two", Mode: callback.ModeSession, Message: "继续"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Session == nil || task.Session.SessionID != "s-two" {
		t.Fatalf("explicit binding = %+v", task.Session)
	}

	updated, err := svc.Update(ctx, task.ID, callback.UpdateInput{
		Session: &callback.SessionInput{SessionID: "s-one", Mode: callback.ModeSession, Message: "改绑"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Session == nil || updated.Session.SessionID != "s-one" ||
		updated.Session.Message != "改绑" {
		t.Fatalf("rebind = %+v", updated.Session)
	}
}

/*
EnsureSession 按 session ID 精确定位：当前叶可用；旧叶归档报 archived，
未归档但已不是当前叶报 superseded，不跟随 compact 新叶。
*/
func TestEnsureSessionUsesExactSession(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	hub := newTestHub()
	fsys := osfs.OS{}
	for _, snap := range []*hooks.SessionSnap{
		{ID: "gen1", LineRoot: "r1", SeedKind: "new", Archived: true},
		{ID: "gen2", LineRoot: "r1", SeedKind: "compress", TargetID: "gen1"},
	} {
		if err := hooks.SaveSnap(ctx, fsys, snap); err != nil {
			t.Fatal(err)
		}
	}
	if err := hub.Topics.UpdateLeaf("r1", "gen2", "线", time.Now().UnixMilli(), 0); err != nil {
		t.Fatal(err)
	}
	topics := &TopicService{Hub: hub, Agents: &AgentService{Hub: hub}}
	sess, err := topics.EnsureSession(ctx, "gen2")
	if err != nil {
		t.Fatal(err)
	}
	if sess.CurrentID() != "gen2" || sess.RootID != "r1" {
		t.Fatalf("loaded %s root %s", sess.CurrentID(), sess.RootID)
	}
	if hub.SessionOf("r1") != sess {
		t.Fatal("line not registered")
	}
	if got := hub.ActiveSession(); got != nil && got.RootID == "r1" {
		t.Fatal("EnsureSession must not activate the line")
	}
	if _, err := topics.EnsureSession(ctx, "gen1"); !errors.Is(err, ErrSessionArchived) {
		t.Fatalf("archived session = %v", err)
	}
}

/* 按计划执行的一次性任务无论成败都停用；失败原因留在 lastResult。 */
func TestCallbackOnceDisablesAfterRun(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	hub := newTestHub()
	store := callback.New(osfs.OS{})
	task, err := store.Create(ctx, callback.Task{
		ID: "cb-once", Title: "一次性", Kind: callback.KindApp, Enabled: true,
		Schedule:  callback.Schedule{Kind: callback.ScheduleOnce, At: "2099-01-01 09:00"},
		App:       &callback.AppTarget{Name: "nope", Callback: "run"},
		NextRunAt: time.Now().Add(time.Hour).UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &CallbackService{Hub: hub, Store: store, now: time.Now}
	svc.run(ctx, task, false)
	got, ok := store.Get(task.ID)
	if !ok {
		t.Fatal("task missing")
	}
	if got.Enabled || got.NextRunAt != 0 || got.LastStatus != callback.StatusError || got.LastResult == "" {
		t.Fatalf("once after run = %+v", got)
	}
	if len(got.Runs) != 1 || got.Runs[0].Status != callback.StatusError || got.Runs[0].Target == "" {
		t.Fatalf("failed run record = %+v", got.Runs)
	}
}

/* 一次性时间已过时创建/启用都拒绝（避免建一个永远不会跑的任务）。 */
func TestCallbackRejectsPastOnce(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	hub := newTestHub()
	svc := &CallbackService{
		Hub: hub, Topics: &TopicService{Hub: hub}, Store: callback.New(osfs.OS{}), now: time.Now,
	}
	_, err := svc.Create(ctx, callback.CreateInput{
		Title:    "过期",
		Kind:     callback.KindSession,
		Schedule: callback.Schedule{Kind: callback.ScheduleOnce, At: "2020-01-01 09:00"},
		Session:  &callback.SessionInput{Mode: callback.ModeNew, Message: "hi"},
	})
	if err == nil {
		t.Fatal("past once must be rejected")
	}
}

/*
快应用任务只存 name + callback：触发时自动启动后端并调用声明端点，
结果写运行记录，同时向通知栏推一条 info。
*/
func TestCallbackAppRunRecordsAndNotice(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	appDir := filepath.Join("apps", "demo")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := fmt.Sprintf(`{"title":"Demo","backend":"python server.py","callbacks":[{"name":"sync","url":%q,"body":{"full":true}}]}`, srv.URL)
	if err := os.WriteFile(filepath.Join(appDir, "app.quick"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	hub := newTestHub()
	term := &fakeTerm{}
	svc := NewCallbackService(hub, nil, nil, &AppsService{Fsys: osfs.OS{}, Term: term}, nil, osfs.OS{})
	svc.now = time.Now
	task, err := svc.Store.Create(ctx, callback.Task{
		ID: "cb-app", Title: "同步", Kind: callback.KindApp, Enabled: true,
		Schedule:  callback.Schedule{Kind: callback.ScheduleInterval, Every: "24h"},
		App:       &callback.AppTarget{Name: "demo", Callback: "sync"},
		NextRunAt: time.Now().Add(time.Hour).UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.run(ctx, task, true)

	got, ok := svc.Store.Get(task.ID)
	if !ok {
		t.Fatal("task missing")
	}
	if got.LastStatus != callback.StatusOK || !strings.Contains(got.LastResult, "HTTP 200") {
		t.Fatalf("run state = %+v", got)
	}
	if len(got.Runs) != 1 {
		t.Fatalf("runs = %+v", got.Runs)
	}
	run := got.Runs[0]
	if run.Status != callback.StatusOK || !strings.Contains(run.Content, srv.URL) || !strings.Contains(run.Content, "POST") {
		t.Fatalf("run record = %+v", run)
	}
	if len(term.calls) != 1 || term.calls[0].Command != "python server.py" {
		t.Fatalf("backend not auto-started: %+v", term.calls)
	}
	notices := svc.Notices()
	if len(notices) != 1 || notices[0].Tool != "同步" ||
		!strings.Contains(notices[0].Args, "HTTP 200") ||
		!strings.Contains(notices[0].Args, "callback-task:"+task.ID) {
		t.Fatalf("notices = %+v", notices)
	}
	svc.DismissNotice(notices[0].CallID)
	if len(svc.Notices()) != 0 {
		t.Fatal("notice not dismissed")
	}
}

/* 通知跳转：成功投递会话指向根 ID，其余指向任务详情；detail 带目标与结果。 */
func TestCallbackNoticeTarget(t *testing.T) {
	svc := &CallbackService{}
	task := callback.Task{ID: "cb-notice", Title: "每日总结", Kind: callback.KindSession}
	svc.addRunNotice(task, callback.StatusOK,
		callbackRun{Target: "会话 s1", RootID: "r1"}, "已发送到会话 s1")
	svc.addRunNotice(task, callback.StatusError,
		callbackRun{Target: "会话 s1"}, "目标会话已归档")

	notices := svc.Notices()
	if len(notices) != 2 {
		t.Fatalf("notices = %+v", notices)
	}
	if !strings.Contains(notices[1].Args, `"target":"callback-session:r1"`) ||
		!strings.Contains(notices[1].Args, "已发送到会话 s1") {
		t.Fatalf("success notice = %s", notices[1].Args)
	}
	if !strings.Contains(notices[0].Args, `"target":"callback-task:cb-notice"`) ||
		!strings.Contains(notices[0].Args, "目标会话已归档") {
		t.Fatalf("failure notice = %s", notices[0].Args)
	}
}

/* 会话已归档：回调失败并留下记录，不向归档库投递。 */
func TestCallbackSessionArchivedFails(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	hub := newTestHub()
	fsys := osfs.OS{}
	if err := hooks.SaveSnap(ctx, fsys, &hooks.SessionSnap{
		ID: "s1", LineRoot: "s1", SeedKind: "new", Archived: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := hub.Topics.UpdateLeaf("s1", "s1", "归档会话", time.Now().UnixMilli(), 0); err != nil {
		t.Fatal(err)
	}
	store := callback.New(fsys)
	task, err := store.Create(ctx, callback.Task{
		ID: "cb-archived", Title: "归档任务", Kind: callback.KindSession, Enabled: true,
		Schedule:  callback.Schedule{Kind: callback.ScheduleInterval, Every: "1h"},
		Session:   &callback.SessionTarget{SessionID: "s1", Mode: callback.ModeSession, Message: "继续"},
		NextRunAt: time.Now().Add(time.Hour).UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &CallbackService{
		Hub: hub, Topics: &TopicService{Hub: hub, Agents: &AgentService{Hub: hub}},
		Store: store, now: time.Now,
	}
	if _, err := svc.Create(ctx, callback.CreateInput{
		Title: "不能建", Kind: callback.KindSession,
		Schedule:    callback.Schedule{Kind: callback.ScheduleInterval, Every: "1h"},
		Session:     &callback.SessionInput{Mode: callback.ModeSession, Message: "hi"},
		BindSession: "s1",
	}); err == nil || !strings.Contains(err.Error(), "已归档") {
		t.Fatalf("create on archived session = %v", err)
	}
	svc.run(ctx, task, true)

	got, ok := store.Get(task.ID)
	if !ok {
		t.Fatal("task missing")
	}
	if got.LastStatus != callback.StatusError || !strings.Contains(got.LastResult, "已归档") {
		t.Fatalf("archived run = %+v", got)
	}
	if len(got.Runs) != 1 || got.Runs[0].Status != callback.StatusError {
		t.Fatalf("archived run record = %+v", got.Runs)
	}
}

/* compact 换代：旧 session ID 不再可用，任务失败且不写到新叶。 */
func TestCallbackSessionSupersededFails(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	hub := newTestHub()
	fsys := osfs.OS{}
	for _, snap := range []*hooks.SessionSnap{
		{ID: "gen1", LineRoot: "r1", SeedKind: "new"},
		{ID: "gen2", LineRoot: "r1", SeedKind: "compress", TargetID: "gen1"},
	} {
		if err := hooks.SaveSnap(ctx, fsys, snap); err != nil {
			t.Fatal(err)
		}
	}
	if err := hub.Topics.UpdateLeaf("r1", "gen2", "线", time.Now().UnixMilli(), 0); err != nil {
		t.Fatal(err)
	}
	store := callback.New(fsys)
	task, err := store.Create(ctx, callback.Task{
		ID: "cb-old", Title: "旧会话", Kind: callback.KindSession, Enabled: true,
		Schedule:  callback.Schedule{Kind: callback.ScheduleInterval, Every: "1h"},
		Session:   &callback.SessionTarget{SessionID: "gen1", Mode: callback.ModeSession, Message: "继续"},
		NextRunAt: time.Now().Add(time.Hour).UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &CallbackService{
		Hub: hub, Topics: &TopicService{Hub: hub, Agents: &AgentService{Hub: hub}},
		Store: store, now: time.Now,
	}
	svc.run(ctx, task, true)

	got, _ := store.Get(task.ID)
	if got.LastStatus != callback.StatusError || !strings.Contains(got.LastResult, "换代") {
		t.Fatalf("superseded run = %+v", got)
	}
	if len(got.Runs) != 1 || got.Runs[0].Status != callback.StatusError {
		t.Fatalf("superseded run record = %+v", got.Runs)
	}
}

/* 脚本任务在工作目录执行命令：成功与失败都写运行记录，结果带输出与退出码。 */
func TestCallbackScriptRun(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	hub := newTestHub()
	svc := NewCallbackService(hub, nil, nil, nil, nil, osfs.OS{})
	svc.now = time.Now

	create := func(id, command string) callback.Task {
		t.Helper()
		task, err := svc.Store.Create(ctx, callback.Task{
			ID: id, Title: id, Kind: callback.KindScript, Enabled: true,
			Schedule:  callback.Schedule{Kind: callback.ScheduleInterval, Every: "1h"},
			Script:    &callback.ScriptTarget{Command: command},
			NextRunAt: time.Now().Add(time.Hour).UnixMilli(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}

	okTask := create("cb-script-ok", "echo hello")
	svc.run(ctx, okTask, true)
	got, _ := svc.Store.Get(okTask.ID)
	if got.LastStatus != callback.StatusOK || !strings.Contains(got.LastResult, "hello") {
		t.Fatalf("script ok = %+v", got)
	}
	if len(got.Runs) != 1 || got.Runs[0].Target != "脚本" || got.Runs[0].Content != "echo hello" {
		t.Fatalf("script run = %+v", got.Runs)
	}

	badTask := create("cb-script-bad", "exit 3")
	svc.run(ctx, badTask, true)
	got, _ = svc.Store.Get(badTask.ID)
	if got.LastStatus != callback.StatusError || !strings.Contains(got.LastResult, "退出码 3") {
		t.Fatalf("script fail = %+v", got)
	}
	notices := svc.Notices()
	if len(notices) != 2 || notices[0].Tool != "cb-script-bad" || notices[1].Tool != "cb-script-ok" {
		t.Fatalf("script notices = %+v", notices)
	}
	if !strings.Contains(notices[0].Args, "callback-task:"+badTask.ID) ||
		!strings.Contains(notices[1].Args, "callback-task:"+okTask.ID) {
		t.Fatalf("script notice targets = %+v", notices)
	}
}

/* 脚本命令为空在创建时拒绝。 */
func TestCallbackScriptRequiresCommand(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	svc := &CallbackService{Hub: newTestHub(), Store: callback.New(osfs.OS{}), now: time.Now}
	_, err := svc.Create(ctx, callback.CreateInput{
		Title: "空脚本", Kind: callback.KindScript,
		Schedule: callback.Schedule{Kind: callback.ScheduleInterval, Every: "1h"},
		Script:   &callback.ScriptTarget{Command: "  "},
	})
	if err == nil {
		t.Fatal("empty script command must be rejected")
	}
}

/* 只改会话消息不换绑定：更新时保留创建时落下的叶 session ID。 */
func TestCallbackUpdateKeepsSessionBinding(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	hub := newTestHub()
	fsys := osfs.OS{}
	snap := &hooks.SessionSnap{
		ID: "s-keep", LineRoot: "r-keep", SeedKind: "new",
		Messages: []types.Message{{Role: types.RoleUser, Content: "已有消息"}},
	}
	if err := hooks.SaveSnap(ctx, fsys, snap); err != nil {
		t.Fatal(err)
	}
	if err := hub.Topics.UpdateLeaf("r-keep", "s-keep", "已有消息", time.Now().UnixMilli(), 1); err != nil {
		t.Fatal(err)
	}
	store := callback.New(fsys)
	task, err := store.Create(ctx, callback.Task{
		ID: "cb-keep", Title: "绑定", Kind: callback.KindSession, Enabled: true,
		Schedule: callback.Schedule{Kind: callback.ScheduleInterval, Every: "1h"},
		Session:  &callback.SessionTarget{SessionID: "s-keep", Mode: callback.ModeSession, Message: "旧消息"},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &CallbackService{
		Hub: hub, Topics: &TopicService{Hub: hub, Agents: &AgentService{Hub: hub}},
		Store: store, now: time.Now,
	}
	updated, err := svc.Update(ctx, task.ID, callback.UpdateInput{
		Session: &callback.SessionInput{Mode: callback.ModeSession, Message: "新消息"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Session == nil || updated.Session.SessionID != "s-keep" ||
		updated.Session.Message != "新消息" {
		t.Fatalf("binding drifted: %+v", updated.Session)
	}
}

/* 改任务类型时清掉另一类目标：前端只提交新目标，旧目标不能残留。 */
func TestCallbackUpdateSwitchesTarget(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	hub := newTestHub()
	fsys := osfs.OS{}
	appDir := filepath.Join("apps", "demo")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.quick"),
		[]byte(`{"title":"Demo","callbacks":[{"name":"sync","url":"http://127.0.0.1:18731/api/sync"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &CallbackService{
		Hub: hub, Topics: &TopicService{Hub: hub}, Store: callback.New(fsys), now: time.Now,
	}
	task, err := svc.Create(ctx, callback.CreateInput{
		Title:    "先会话",
		Kind:     callback.KindSession,
		Schedule: callback.Schedule{Kind: callback.ScheduleInterval, Every: "1h"},
		Session:  &callback.SessionInput{Mode: callback.ModeNew, Message: "hi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	kind := callback.KindApp
	updated, err := svc.Update(ctx, task.ID, callback.UpdateInput{
		Kind: &kind,
		App:  &callback.AppTarget{Name: "demo", Callback: "sync"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Kind != callback.KindApp || updated.Session != nil || updated.App == nil {
		t.Fatalf("switched task = %+v", updated)
	}
}
