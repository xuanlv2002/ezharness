package tools

import (
	"context"
	"strings"
	"testing"

	"ezharness/core/internal/callback"
)

/* fakeCallbackAPI 记录工具调用并返回固定任务。 */
type fakeCallbackAPI struct {
	tasks   []callback.Task
	created callback.CreateInput
	updated callback.UpdateInput
	enabled *bool
	deleted string
	ran     string
}

func (f *fakeCallbackAPI) List() []callback.Task { return f.tasks }

func (f *fakeCallbackAPI) Create(_ context.Context, in callback.CreateInput) (callback.Task, error) {
	f.created = in
	return callback.Task{ID: "cb-new", Title: in.Title, Kind: in.Kind}, nil
}

func (f *fakeCallbackAPI) Update(_ context.Context, id string, in callback.UpdateInput) (callback.Task, error) {
	f.updated = in
	return callback.Task{ID: id, Title: "updated"}, nil
}

func (f *fakeCallbackAPI) Delete(_ context.Context, id string) error {
	f.deleted = id
	return nil
}

func (f *fakeCallbackAPI) SetEnabled(_ context.Context, id string, enabled bool) (callback.Task, error) {
	f.enabled = &enabled
	return callback.Task{ID: id, Title: "t", Enabled: enabled}, nil
}

func (f *fakeCallbackAPI) RunNow(id string) error {
	f.ran = id
	return nil
}

/* list 标注绑定当前分支的任务，并带上计划与目标。 */
func TestCallbackToolList(t *testing.T) {
	api := &fakeCallbackAPI{tasks: []callback.Task{{
		ID: "cb-1", Title: "每日总结", Kind: callback.KindSession, Enabled: true,
		Schedule: callback.Schedule{Kind: callback.ScheduleCron, Cron: "0 9 * * *"},
		Session:  &callback.SessionTarget{SessionID: "s1", Mode: callback.ModeSession, Message: "hi"},
	}}}
	out, err := Callback(api, func() string { return "s1" }).Invoke(context.Background(), []byte(`{"action":"list"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cb-1", "每日总结", "本会话", "每天 09:00"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list output missing %q: %s", want, out)
		}
	}
}

/* create 把工具参数透传给服务；update/enable/run 的契约同样走窄接口。 */
func TestCallbackToolActions(t *testing.T) {
	api := &fakeCallbackAPI{}
	tool := Callback(api, func() string { return "s1" })
	if _, err := tool.Invoke(context.Background(), []byte(`{
		"action":"create","title":"提醒","kind":"session",
		"schedule":{"kind":"interval","every":"30m"},
		"session":{"mode":"new","message":"hello"}}`)); err != nil {
		t.Fatal(err)
	}
	if api.created.Title != "提醒" || api.created.Kind != callback.KindSession ||
		api.created.Schedule.Every != "30m" || api.created.Session == nil ||
		api.created.Session.Mode != callback.ModeNew {
		t.Fatalf("create input = %+v", api.created)
	}
	if _, err := tool.Invoke(context.Background(), []byte(`{"action":"update","id":"cb-1","title":"新标题"}`)); err != nil {
		t.Fatal(err)
	}
	if api.updated.Title == nil || *api.updated.Title != "新标题" {
		t.Fatalf("update input = %+v", api.updated)
	}
	if _, err := tool.Invoke(context.Background(), []byte(`{"action":"disable","id":"cb-1"}`)); err != nil {
		t.Fatal(err)
	}
	if api.enabled == nil || *api.enabled {
		t.Fatalf("enabled = %v", api.enabled)
	}
	if _, err := tool.Invoke(context.Background(), []byte(`{"action":"run","id":"cb-1"}`)); err != nil || api.ran != "cb-1" {
		t.Fatalf("run = %q %v", api.ran, err)
	}
	if _, err := tool.Invoke(context.Background(), []byte(`{"action":"delete","id":"cb-1"}`)); err != nil || api.deleted != "cb-1" {
		t.Fatalf("delete = %q %v", api.deleted, err)
	}
}

/* create 的 session 模式由工具带上当前会话：agent 不需要知道叶 ID。 */
func TestCallbackToolBindsCurrentSession(t *testing.T) {
	api := &fakeCallbackAPI{}
	_, err := Callback(api, func() string { return "s9" }).Invoke(context.Background(), []byte(`{
		"action":"create","title":"当前会话","kind":"session",
		"schedule":{"kind":"interval","every":"30m"},
		"session":{"mode":"session","message":"hello"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if api.created.Session == nil || api.created.Session.Mode != callback.ModeSession ||
		api.created.BindSession != "s9" {
		t.Fatalf("session input = %+v bind=%q", api.created.Session, api.created.BindSession)
	}
}

/* script 目标透传命令；create 的脚本任务不需要会话。 */
func TestCallbackToolScript(t *testing.T) {
	api := &fakeCallbackAPI{}
	_, err := Callback(api, func() string { return "s1" }).Invoke(context.Background(), []byte(`{
		"action":"create","title":"同步","kind":"script",
		"schedule":{"kind":"cron","cron":"0 9 * * *"},
		"script":{"command":"./sync.sh"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if api.created.Kind != callback.KindScript || api.created.Script == nil ||
		api.created.Script.Command != "./sync.sh" {
		t.Fatalf("script input = %+v", api.created.Script)
	}
	if api.created.BindSession != "s1" {
		t.Fatalf("tool must pass caller session: %q", api.created.BindSession)
	}
}
