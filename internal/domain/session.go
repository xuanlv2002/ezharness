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
	"sort"
	"sync"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/ext/hook/taskplan"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/hooks"
	"ezharness/internal/osfs"
)

/* ErrBusy 表示会话当前有一轮运行未结束。 */
var ErrBusy = errors.New("session busy")

/* ErrNoAPIKey 表示 models.json 尚未配置 API Key（应用可启动，发消息被拒）。 */
var ErrNoAPIKey = errors.New("未配置 API Key，请在设置中填写")

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
	ID     string
	Fsys   osfs.OS
	Sess   *hooks.Store
	Topics *hooks.Topics

	mu         sync.Mutex
	history    []types.Message
	cur        *runState
	subs       map[chan []byte]struct{}
	pending    map[string]Event
	ctxTokens  int
	wired      *Wiring
	snap       *hooks.SessionSnap // bootstrap 恢复的快照（Assemble 读取；nil = 新建）
	sysP       *hooks.SysPrompt   // system 来源（Assemble 创建；resume/compact 热更）
	turnFrames [][]byte           // 本轮聚合帧缓存（刷新回放重建时间线；轮开始清空）
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

/* SetIdentity 同步会话标识（话题轮换/compact 后）。 */
func (s *Session) SetIdentity(id string) {
	s.mu.Lock()
	s.ID = id
	s.mu.Unlock()
}

/* SetSysP 记录 system 来源（Assemble 注入；空闲期调用）。 */
func (s *Session) SetSysP(sys *hooks.SysPrompt) {
	s.mu.Lock()
	s.sysP = sys
	s.mu.Unlock()
}

/* SysPromptRef 返回 system 来源（nil 表示尚未组装）。 */
func (s *Session) SysPromptRef() *hooks.SysPrompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sysP
}

/* Snapshot 返回 bootstrap 恢复的快照（nil = 新建会话）。 */
func (s *Session) Snapshot() *hooks.SessionSnap {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
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
	s.turnFrames = nil
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

/* Publish 向全部订阅者扇出事件帧；人机请求登记 pending 供断线重放，
聚合帧缓存进 turnFrames 供整轮回放（刷新重建时间线）。 */
func (s *Session) Publish(e Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	s.mu.Lock()
	if id, ok := decisionCallID(e); ok {
		s.pending[id] = e
	}
	if replayable(e.Type) {
		s.turnFrames = append(s.turnFrames, data)
	}
	for ch := range s.subs {
		select {
		case ch <- data:
		default: // 慢消费者丢帧（前端有 turn_end 校正兜底）
		}
	}
	s.mu.Unlock()
}

/* replayable 判定帧是否进回放缓存：聚合帧保留，高频增量与瞬态帧跳过。 */
func replayable(t string) bool {
	switch t {
	case "model_chunk", "reasoning_chunk", "tool_chunk", "model_start",
		"status.snapshot", "turn_end", "loop_end", "iteration_end":
		return false
	}
	return true
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

/*
ReplayFrames 返回 SSE 建立时的重放帧：轮进行中回放整轮聚合帧
（user 输入/模型回复/工具卡/决策——刷新后时间线完整重建；已决
审批由其后的 decision.resolved 帧纠正），空闲时退化为未决请求帧。
*/
func (s *Session) ReplayFrames() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur == nil {
		out := make([][]byte, 0, len(s.pending))
		for _, e := range s.pending {
			if data, err := json.Marshal(e); err == nil {
				out = append(out, data)
			}
		}
		return out
	}
	out := make([][]byte, 0, len(s.turnFrames))
	return append(out, s.turnFrames...) // 未决请求帧也在其中（request 是聚合帧）
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
	Models   ModelsConfig
	Fsys     osfs.OS
	Settings Settings
	Stats    *Stats
	Topics   *hooks.Topics
	Active   *Session
}

/* NewHub 创建领域根：加载配置记录（缺失文件自动创建默认）与累计生命体征，并恢复活动会话。 */
func NewHub() *Hub {
	h := &Hub{Fsys: osfs.OS{}}
	h.Models = ensureModelsConfig(h.Fsys)
	h.Settings = ensureSettings(h.Fsys)
	h.Stats = NewStats(h.Fsys)
	migrateLegacyMemory(h.Fsys)
	h.Topics = hooks.NewTopics(h.Fsys)
	h.Active = h.bootstrap()
	return h
}

/* ensureModelsConfig 加载 models.json（含旧扁平迁移），文件不存在则写盘默认（零配置首启自动创建）。 */
func ensureModelsConfig(fsys osfs.OS) ModelsConfig {
	if _, err := fsys.Read(context.Background(), "models.json"); err != nil {
		m := DefaultModelsConfig()
		_ = SaveModelsConfig(fsys, m)
		return m
	}
	return LoadModelsConfig(fsys)
}

/* migrateLegacyMemory 旧版单文件记忆迁移：memory.md → memory/longterm/harness.md。 */
func migrateLegacyMemory(fsys osfs.OS) {
	if _, err := fsys.Read(context.Background(), hooks.HarnessMd); err == nil {
		return // 已有新布局
	}
	if data, err := fsys.Read(context.Background(), "memory.md"); err == nil && len(data) > 0 {
		_ = fsys.Write(context.Background(), hooks.HarnessMd, data)
	}
}

/* ensureSettings 加载 settings.json，文件不存在则写盘默认。 */
func ensureSettings(fsys osfs.OS) Settings {
	if _, err := fsys.Read(context.Background(), "settings.json"); err != nil {
		st := DefaultSettings()
		_ = SaveSettings(fsys, st)
		return st
	}
	return LoadSettings(fsys)
}

/*
bootstrap 恢复最近修改且未封存的存档，没有则新建。旧版平铺
sessions/<id>.json 不在候选内（ListMain 只认目录项），共存不崩。
恢复的 systemPrompt 不重新组装：快照里的 base/summary 直接注入
SysPrompt，记忆/skill/mcp 变更等到下个 session 才生效。
*/
func (h *Hub) bootstrap() *Session {
	ctx := context.Background()
	ids, _ := hooks.ListMain(ctx, h.Fsys)
	type cand struct {
		id string
		mt int64
	}
	cands := make([]cand, 0, len(ids))
	for _, id := range ids {
		if fi, err := os.Stat(filepath.Join(hooks.SessionsDir, id, "session.json")); err == nil {
			cands = append(cands, cand{id, fi.ModTime().UnixMilli()})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mt > cands[j].mt })
	for _, c := range cands {
		snap, err := hooks.LoadSnap(ctx, h.Fsys, c.id)
		if err != nil || snap.Archived {
			continue
		}
		s := h.newSession(c.id, snap)
		s.setHistory(snap.Messages)
		s.Sess.SetResSnap(snap.Snapshot)
		s.Sess.SetLastOutputAt(snap.LastOutputAt)
		if snap.PrevSession != "" {
			s.Sess.SetPrev(snap.PrevSession, snap.CompactSummary) // compact 链（上翻懒加载用）
		}
		return s
	}
	return h.newSession(hooks.NewSessionID(), nil)
}

func (h *Hub) newSession(id string, snap *hooks.SessionSnap) *Session {
	s := &Session{
		ID:     id,
		Fsys:   h.Fsys,
		Sess:   hooks.NewStore(h.Fsys, id),
		Topics: h.Topics,
		snap:   snap,
		subs:   map[chan []byte]struct{}{},
		pending: map[string]Event{},
	}
	return s
}

/* ModelsSnapshot 返回当前模型四槽。 */
func (h *Hub) ModelsSnapshot() ModelsConfig {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.Models
}

/* ApplyModels 更新模型四槽（持久化由 service 层完成）。 */
func (h *Hub) ApplyModels(m ModelsConfig) {
	h.mu.Lock()
	h.Models = m
	h.mu.Unlock()
}

/* RecordUsage 累计一轮用量到主模型条目并落盘（tokens=prompt+completion）。 */
func (h *Hub) RecordUsage(u *types.Usage) {
	if u == nil {
		return
	}
	h.mu.Lock()
	if e := h.Models.ActiveMain(); e != nil {
		e.Tokens += u.PromptTokens + u.CompletionTokens
	}
	models := h.Models
	h.mu.Unlock()
	_ = SaveModelsConfig(h.Fsys, models)
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
