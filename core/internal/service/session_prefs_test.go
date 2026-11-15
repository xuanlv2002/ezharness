package service

import (
	"context"
	"testing"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
	"ezharness/core/internal/osfs"
)

/*
会话偏好（思考/审批/主模型）随会话走：SetPrefs 立即落 prefs.json、状态快照
返回本会话值、新线继承当前会话、恢复分支从快照字段还原；模型被关闭时
解析回落全局默认。覆盖「不再全局一套」的核心契约。
*/
func TestSessionPrefsPerSession(t *testing.T) {
	chdirTemp(t)
	ctx := context.Background()
	hub := newTestHub()
	hub.ApplyModels(domain.ModelsConfig{Main: []domain.ModelEntry{
		{Name: "m1", APIKey: "k1", Enabled: true},
		{Name: "m2", APIKey: "k2"},
	}})
	agents := &AgentService{Hub: hub}
	svc := &SessionService{Hub: hub, Agents: agents}
	topics := &TopicService{Hub: hub, Agents: agents}

	src := hub.ActiveSession()
	high, strict, m2 := "high", "strict", "m2"
	got, err := svc.SetPrefs(ctx, src.RootID, SessionPrefsPatch{
		ReasoningEffort: &high, ApproveMode: &strict, Model: &m2,
	})
	if err != nil {
		t.Fatalf("SetPrefs: %v", err)
	}
	if !got.OK || got.Model != "m2" || got.ReasoningEffort != "high" || got.ApproveMode != "strict" {
		t.Fatalf("prefs view = %+v", got)
	}

	// 立即落盘（独立文件，不等下一轮）
	p, ok := hooks.LoadPrefs(ctx, osfs.OS{}, src.ID)
	if !ok || p.ActiveModel != "m2" || p.ReasoningEffort != "high" || p.ApproveMode != "strict" {
		t.Fatalf("persisted prefs = %+v ok=%v", p, ok)
	}

	// 状态快照按本会话口径
	st := svc.Snapshot(ctx)
	if st.Model != "m2" || st.ReasoningEffort != "high" || st.ApproveMode != "strict" {
		t.Fatalf("status prefs = model %q effort %q approve %q", st.Model, st.ReasoningEffort, st.ApproveMode)
	}

	// 新线继承来源会话（不是全局设置）
	nb := topics.NewBranch()
	if nb.Sess.Prefs() != (hooks.Prefs{ReasoningEffort: "high", ApproveMode: "strict", ActiveModel: "m2"}) {
		t.Fatalf("new branch prefs = %+v", nb.Sess.Prefs())
	}
	if st := svc.Snapshot(ctx); st.Model != "m2" || st.ReasoningEffort != "high" {
		t.Fatalf("new branch status = %+v", st)
	}

	// 恢复分支：没有 prefs.json 时回落 session.json 快照字段
	snap := &hooks.SessionSnap{
		ID: "fk1", LineRoot: "fk1", SeedKind: "fork",
		ReasoningEffort: "low", ApproveMode: "off", ActiveModel: "m2",
	}
	restored := hub.LoadBranch("fk1", snap)
	if restored.Sess.Prefs().ReasoningEffort != "low" || restored.Sess.Prefs().ApproveMode != "off" || restored.Sess.Prefs().ActiveModel != "m2" {
		t.Fatalf("restored prefs = %+v", restored.Sess.Prefs())
	}

	// prefs.json 优先于快照字段（轮内切换后立即生效，不必等下一轮落盘）
	if err := hooks.SavePrefs(ctx, osfs.OS{}, "fk1", hooks.Prefs{ReasoningEffort: "max", ApproveMode: "", ActiveModel: "m1"}); err != nil {
		t.Fatal(err)
	}
	again := hub.LoadBranch("fk1", snap)
	if p := again.Sess.Prefs(); p.ReasoningEffort != "max" || p.ApproveMode != "" || p.ActiveModel != "m1" {
		t.Fatalf("prefs.json must win over snapshot: %+v", p)
	}

	// 模型被关闭：解析回落全局默认 m1（会话偏好保留，重新开启后恢复）
	m := hub.ModelsSnapshot()
	m.Main[1].Disabled = true
	hub.ApplyModels(m)
	hub.SetActive(src)
	if st := svc.Snapshot(ctx); st.Model != "m1" {
		t.Fatalf("disabled model must fall back to default, got %q", st.Model)
	}
	if src.Sess.Prefs().ActiveModel != "m2" {
		t.Fatal("session choice must survive model disable")
	}

	// 非法模型/档位拒绝
	bad := "missing"
	if _, err := svc.SetPrefs(ctx, src.RootID, SessionPrefsPatch{Model: &bad}); err == nil {
		t.Fatal("unknown model must be rejected")
	}
}
