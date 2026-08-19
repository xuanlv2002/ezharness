/*
Package domain 是 ezharness 的领域层：会话聚合（并发状态机）与事件帧。
零 HTTP 依赖——表现层（controller）只消费，不碰内部状态。
*/
package domain

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/ext/hook/localsession"
	"github.com/xuanlv2002/ezloop/ext/hook/taskplan"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/config"
	"ezharness/internal/hooks"
	"ezharness/internal/osfs"
)

/* ErrBusy 表示会话当前有一轮运行未结束。 */
var ErrBusy = errors.New("session busy")

/* runState 是一轮运行的生命周期句柄。 */
type runState struct {
	ctx    context.Context
	cancel context.CancelFunc
}

/* Wiring 是一次 agent 装配的注入物（由 service 层构造）。 */
type Wiring struct {
	Agent     *core.Agent
	Provider  ModelProvider
	ApproveCh chan<- approve.Decision
	AnswerCh  chan<- askuser.Answer
	PlanCh    chan<- taskplan.Decision
	ToolNames []string
}

/* ModelProvider 是领域所需的最小模型面（摘要用例），ezloop 同名接口的子集。 */
type ModelProvider interface {
	Invoke(ctx context.Context, req *types.ModelRequest) (*types.ModelResponse, error)
}

/* Session 是会话聚合：history、当前轮、SSE 订阅、未决请求。 */
type Session struct {
	ID    string
	Cfg   config.Config
	Fsys  osfs.OS
	Sess  *localsession.Hook
	Topics *hooks.Topics

	mu        sync.Mutex
	history   []types.Message
	cur       *runState
	subs      map[chan []byte]struct{}
	pending   map[string]Event
	ctxTokens int
	wired     *Wiring
}

/* Attach 注入装配产物（service 层构造，领域持有引用）。 */
func (s *Session) Attach(w Wiring) {
	s.mu.Lock()
	s.wired = &w
	s.mu.Unlock()
}

/* Wired 返回装配产物（可能为 nil，表示尚未组装）。 */
func (s *Session) Wired() *Wiring {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wired
}

/* History 返回消息历史副本。 */
func (s *Session) History() []types.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]types.Message, len(s.history))
	copy(out, s.history)
	return out
}

/* setHistory 原子替换历史（轮结束/恢复话题）。 */
func (s *Session) setHistory(msgs []types.Message) {
	s.mu.Lock()
	s.history = msgs
	s.mu.Unlock()
}

/* ReplaceHistory 原子替换历史（service 层恢复话题用例调用）。 */
func (s *Session) ReplaceHistory(msgs []types.Message) { s.setHistory(msgs) }

/* Busy 报告是否有一轮运行中。 */
func (s *Session) Busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur != nil
}

/* CtxTokens 返回最近一次模型调用的上下文长度。 */
func (s *Session) CtxTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctxTokens
}

/* SetCtxTokens 更新水位（事件消费侧调用）。 */
func (s *Session) SetCtxTokens(n int) {
	s.mu.Lock()
	s.ctxTokens = n
	s.mu.Unlock()
}

/* SetIdentity 同步会话标识（话题轮换后）。 */
func (s *Session) SetIdentity(id string) {
	s.mu.Lock()
	s.ID = id
	s.mu.Unlock()
}

/* StartRun 占用当前轮并返回运行上下文（busy 时返回 ErrBusy）。 */
func (s *Session) StartRun(ctx context.Context, text string) (*core.RunHandle, context.CancelFunc, error) {
	s.mu.Lock()
	if s.cur != nil {
		s.mu.Unlock()
		return nil, nil, ErrBusy
	}
	if s.wired == nil {
		s.mu.Unlock()
		return nil, nil, errors.New("session not assembled")
	}
	turnCtx, cancel := context.WithCancel(ctx)
	h := s.wired.Agent.RunAsync(turnCtx, text, core.WithHistory(s.history...))
	s.cur = &runState{ctx: turnCtx, cancel: cancel}
	s.mu.Unlock()
	return h, cancel, nil
}

/* FinishRun 结束当前轮：更新历史、清未决请求、释放占用。
返回本轮是否发生了状态更新（state 非 nil 时历史以 state 为准）。 */
func (s *Session) FinishRun(state *types.LoopState, runErr error) {
	s.mu.Lock()
	if state != nil && runErr == nil {
		s.history = state.Messages
	}
	s.cur = nil
	s.pending = map[string]Event{}
	s.mu.Unlock()
}

/* Cancel 取消当前轮。 */
func (s *Session) Cancel() {
	s.mu.Lock()
	if s.cur != nil {
		s.cur.cancel()
	}
	s.mu.Unlock()
}

/* sendDecision 异步回传决策：hook 阻塞在 channel 上，同步发送会死锁。 */
func sendDecision[T any](s *Session, ch chan<- T, v T, callID string) {
	s.clearPending(callID)
	s.mu.Lock()
	cur := s.cur
	s.mu.Unlock()
	if cur == nil {
		return // 轮已结束，过期决策丢弃
	}
	go func() {
		select {
		case ch <- v:
		case <-cur.ctx.Done():
		}
	}()
}

/* DecideApprove 回传审批决策。 */
func (s *Session) DecideApprove(d approve.Decision) {
	w := s.Wired()
	if w == nil {
		return
	}
	sendDecision(s, w.ApproveCh, d, d.CallID)
}

/* DecideAnswer 回传提问回答。 */
func (s *Session) DecideAnswer(a askuser.Answer) {
	w := s.Wired()
	if w == nil {
		return
	}
	sendDecision(s, w.AnswerCh, a, a.CallID)
}

/* DecidePlan 回传规划处置。 */
func (s *Session) DecidePlan(d taskplan.Decision) {
	w := s.Wired()
	if w == nil {
		return
	}
	sendDecision(s, w.PlanCh, d, d.CallID)
}

/* ── SSE 订阅（领域事件出口，HTTP 帧写出在 controller）── */

/* Subscribe 注册一个订阅者，返回事件 channel 与注销函数。 */
func (s *Session) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 1024)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

/* Publish 向全部订阅者扇出事件帧；人机请求登记 pending 供断线重放。 */
func (s *Session) Publish(e Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	s.mu.Lock()
	if id, ok := decisionCallID(e); ok {
		s.pending[id] = e
	}
	for ch := range s.subs {
		select {
		case ch <- data:
		default: // 慢消费者丢帧（前端有 turn_end 校正兜底）
		}
	}
	s.mu.Unlock()
}

/* PendingFrames 返回未决请求帧快照（SSE 建立时重放）。 */
func (s *Session) PendingFrames() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, 0, len(s.pending))
	for _, e := range s.pending {
		if data, err := json.Marshal(e); err == nil {
			out = append(out, data)
		}
	}
	return out
}

/* clearPending 清除一个已回传的请求。 */
func (s *Session) clearPending(callID string) {
	s.mu.Lock()
	delete(s.pending, callID)
	s.mu.Unlock()
}

/* decisionCallID 提取人机请求帧的工具调用 ID。 */
func decisionCallID(e Event) (string, bool) {
	switch e.Type {
	case "approve.request", "askuser.request", "taskplan.request":
		var d struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(e.Data, &d) == nil && d.ID != "" {
			return d.ID, true
		}
	}
	return "", false
}

/* ── Hub：设置、话题索引与唯一活动会话 ── */

/* Hub 管理应用级单例状态。 */
type Hub struct {
	mu       sync.Mutex
	Cfg      config.Config
	Fsys     osfs.OS
	Settings Settings
	Topics   *hooks.Topics
	Active   *Session
}

/* NewHub 创建领域根并恢复活动会话。 */
func NewHub(c config.Config) *Hub {
	h := &Hub{Cfg: c, Fsys: osfs.OS{}}
	h.Settings = LoadSettings(h.Fsys, DefaultSettings(c.Model, c.BaseURL))
	h.Topics = hooks.NewTopics(h.Fsys)
	h.Active = h.bootstrap()
	return h
}

/* bootstrap 恢复最近修改的存档，没有则新建。 */
func (h *Hub) bootstrap() *Session {
	ids, _ := localsession.List(context.Background(), h.Fsys, "")
	latest, latestMt := "", int64(-1)
	for _, id := range ids {
		if strings.Contains(id, "-task-") {
			continue // fork 分流文件不作恢复候选
		}
		if fi, err := os.Stat(filepath.Join("sessions", id+".json")); err == nil && fi.ModTime().UnixMilli() > latestMt {
			latest, latestMt = id, fi.ModTime().UnixMilli()
		}
	}
	if latest != "" {
		s := h.newSession(latest)
		if msgs, err := localsession.Load(context.Background(), h.Fsys, "", latest); err == nil {
			s.setHistory(msgs.Messages)
		}
		return s
	}
	return h.newSession(localsession.NewID())
}

func (h *Hub) newSession(id string) *Session {
	return &Session{
		ID:      id,
		Cfg:     h.Cfg,
		Fsys:    h.Fsys,
		Sess:    localsession.New(h.Fsys, id),
		Topics:  h.Topics,
		subs:    map[chan []byte]struct{}{},
		pending: map[string]Event{},
	}
}

/* SettingsSnapshot 返回当前设置。 */
func (h *Hub) SettingsSnapshot() Settings {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.Settings
}

/* ApplySettings 更新设置（持久化由 service 层完成）。 */
func (h *Hub) ApplySettings(s Settings) {
	h.mu.Lock()
	h.Settings = s
	h.mu.Unlock()
}

/* ReplaceActive 重建活动会话对象（当前仅新建场景）。 */
func (h *Hub) ReplaceActive(s *Session) {
	h.mu.Lock()
	h.Active = s
	h.mu.Unlock()
}

/* event 包别名（供 service 引用统一出口）。 */
type E = event.Event
