package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/domain"
)

/* blockingProvider 一直等到轮上下文被取消；记录调用次数与最后一条 user 文本。 */
type blockingProvider struct {
	mu    sync.Mutex
	calls int
	last  string
}

func (p *blockingProvider) Invoke(ctx context.Context, req *types.ModelRequest) (*types.ModelResponse, error) {
	p.mu.Lock()
	p.calls++
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == types.RoleUser {
			p.last = req.Messages[i].Content
			break
		}
	}
	p.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (p *blockingProvider) snapshot() (int, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, p.last
}

/* 新消息打到正在跑轮的会话：ChatService 先取消旧轮，再启动新轮。 */
func TestChatSendInterruptsRunningTurn(t *testing.T) {
	chdirTemp(t)
	hub := newTestHub()
	hub.ApplyModels(domain.ModelsConfig{Main: []domain.ModelEntry{{
		Name: "fake", APIKey: "test", Enabled: true,
	}}})
	p := &blockingProvider{}
	sess := hub.CreateBranch()
	sess.Attach(domain.Wiring{Agent: core.NewAgent(p)})
	hub.SetActive(sess)
	svc := &ChatService{Hub: hub}

	if err := svc.Send(sess.RootID, "first", nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 2*time.Second, sess.Busy)
	if err := svc.Send(sess.RootID, "second", nil); err != nil {
		t.Fatalf("interrupting send: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		calls, _ := p.snapshot()
		return calls >= 2
	})
	calls, last := p.snapshot()
	if calls < 2 || !strings.Contains(last, "second") {
		t.Fatalf("provider calls=%d last=%q", calls, last)
	}
	svc.Cancel(sess.RootID)
	waitUntil(t, 2*time.Second, func() bool { return !sess.Busy() })
}

func waitUntil(t *testing.T, limit time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
