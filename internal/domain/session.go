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
	"time"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
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
	ToolNames []string
	Trace     *hooks.Trace // 调用链记录（Resume 切会话时同步切 trace）
}

/* ModelProvider 是领域所需的最小模型面（摘要用例），ezloop 同名接口的子集。 */
type ModelProvider interface {
	Invoke(ctx context.Context, req *types.ModelRequest) (*types.ModelResponse, error)
}

/* Session 是会话聚合：history、当前轮、SSE 订阅、未决请求。
ID = 当前叶 session ID（compact 换代随之更新）；RootID = 所属分支根
（稳定，注册表键与前端路由用它）。 */
type Session struct {
	ID      string
	RootID  string
	Fsys    osfs.OS
	Sess    *hooks.Store
	Topics  *hooks.Topics

	mu         sync.Mutex
	history    []types.Message
	archiving  bool // 归档进行中（摘要最长 2 分钟）：锁发消息/切分支/二次归档
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

/*
modelView 返回发给模型的历史：最后一个 <context_trim> marker（含）
之后的消息——marker 携带折叠段摘要，是新旧上下文的衔接点；marker
之前的不进上下文。无 marker 时全量（含 fork seed 前缀语义不变）。
*/
func (s *Session) modelView() []types.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return modelViewLocked(s.history)
}

func modelViewLocked(history []types.Message) []types.Message {
	start := hooks.ViewStart(history)
	if start == 0 && len(history) > 0 && history[0].Role == types.RoleSystem {
		start = 1 // system 由 sys hook 重注，不随视图携带
	}
	return history[start:]
}

/* ModelView 返回发给模型的上下文视图（归档摘要的输入，含 marker 摘要链）。 */
func (s *Session) ModelView() []types.Message { return s.modelView() }

/* Busy 报告是否有一轮运行中。 */
func (s *Session) Busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur != nil
}

/* BeginArchive 原子占归档锁（摘要期间锁对话/切分支/防二次触发），已占用返回 false。 */
func (s *Session) BeginArchive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.archiving || s.cur != nil {
		return false
	}
	s.archiving = true
	return true
}

/* EndArchive 释放归档锁。 */
func (s *Session) EndArchive() {
	s.mu.Lock()
	s.archiving = false
	s.mu.Unlock()
}

/* Archiving 报告归档是否进行中。 */
func (s *Session) Archiving() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.archiving
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

/* SetIdentity 同步会话标识（话题轮换/compact 后），水位清零
（恢复场景由调用方随后按快照重注）。RootID 不变：换代不换线。 */
func (s *Session) SetIdentity(id string) {
	s.mu.Lock()
	s.ID = id
	s.ctxTokens = 0
	s.mu.Unlock()
}

/* RotateTo 换代（归档后新库空置起步）：切标识、清历史、水位清零。 */
func (s *Session) RotateTo(id string) {
	s.mu.Lock()
	s.ID = id
	s.ctxTokens = 0
	s.history = nil
	s.mu.Unlock()
}

/* RootLocked 返回所属分支根 ID。 */
func (s *Session) Root() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.RootID
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

/* StartRun 占用当前轮并返回运行上下文（busy/归档中返回 ErrBusy）。 */
func (s *Session) StartRun(ctx context.Context, text string, refs []hooks.RefFile) (*core.RunHandle, context.CancelFunc, error) {
	s.mu.Lock()
	if s.cur != nil || s.archiving {
		s.mu.Unlock()
		return nil, nil, ErrBusy
	}
	if s.wired == nil {
		s.mu.Unlock()
		return nil, nil, errors.New("session not assembled")
	}
	turnCtx, cancel := context.WithCancel(ctx)
	// 锁内取模型视图须用无锁版本（modelView 自身抢 s.mu，重入即死锁）
	h := s.wired.Agent.RunAsync(turnCtx, text,
		core.WithHistory(modelViewLocked(s.history)...),
		hooks.WithRefFiles(refs))
	s.cur = &runState{ctx: turnCtx, cancel: cancel}
	s.turnFrames = nil
	s.mu.Unlock()
	return h, cancel, nil
}

/*
FinishRun 结束当前轮：更新历史、清未决请求、释放占用。
历史以 state 为准（含手动取消/出错轮）：引擎保证每个已入史的 tool_call
都有结果消息，取消轮的部分输出也保留--sessionstore 落盘的与内存的
必须一致，否则同进程续聊丢上下文（重启反而恢复）。
*/
func (s *Session) FinishRun(state *types.LoopState, runErr error) {
	s.mu.Lock()
	if state != nil {
		// trim 轮内截断过 state.Messages：与上轮全量 MergeFull（marker 前档案保留）
		if len(hooks.FoldedOf(state)) > 0 {
			s.history = hooks.MergeFull(s.history, state)
		} else {
			s.history = state.Messages
		}
	}
	s.cur = nil
	s.pending = map[string]Event{}
	s.mu.Unlock()
}

/* Cancel 取消当前轮；无运行轮时补发一帧 turn_end（幂等纠正——轮在
SSE 断线窗口内结束时前端会错过 turn_end 而卡在"运行中"，取消操作
借此自愈）。 */
func (s *Session) Cancel() {
	s.mu.Lock()
	idle := s.cur == nil
	if s.cur != nil {
		s.cur.cancel()
	}
	s.mu.Unlock()
	if idle {
		s.Publish(TurnEnd("cancelled", 0, nil, nil, 0))
	}
}

/* TurnActive 返回是否有轮在运行（SSE 建连 replay.sync 用）。 */
func (s *Session) TurnActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur != nil
}

/* Shutdown 收尾运行中的轮：取消并等待轮结束落盘（带超时，进程退出/
换代用——轮内历史只在 OnEnd 落盘，不等待直接退出会丢整轮）。 */
func (s *Session) Shutdown(wait time.Duration) {
	s.Cancel()
	deadline := time.Now().Add(wait)
	for s.Busy() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
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

/* replayable 判定帧是否进回放缓存：聚合帧保留，高频增量与瞬态帧跳过
（res.change 与 status.snapshot 同理：对应块插在 pendingUser 截断点
之前，回放重插会重复渲染）。 */
func replayable(t string) bool {
	switch t {
	case "model_chunk", "reasoning_chunk", "tool_chunk", "model_start",
		"status.snapshot", "res.change", "turn_end", "loop_end", "iteration_end":
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

/* PendingNotice 是通知栏全局条目（GET /api/notifications 的域模型）。 */
type PendingNotice struct {
	CallID string `json:"callId"`
	ForkID string `json:"forkId,omitempty"`
	Kind   string `json:"kind"` // approve | ask
	Tool   string `json:"tool"`
	Args   string `json:"args,omitempty"`
	Ts     int64  `json:"ts"`
}

/* PendingNotices 返回未决人机请求快照（通知栏跨分支轮询数据源）。
按请求时间降序（新在前）——pending 是 map，遍历序随机，固定排序保证
轮询结果稳定不抖动。 */
func (s *Session) PendingNotices() []PendingNotice {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PendingNotice, 0, len(s.pending))
	for _, e := range s.pending {
		kind := ""
		switch e.Type {
		case "approve.request":
			kind = "approve"
		case "askuser.request":
			kind = "ask"
		default:
			continue
		}
		var d ToolStartData
		if json.Unmarshal(e.Data, &d) != nil || d.ID == "" {
			continue
		}
		out = append(out, PendingNotice{CallID: d.ID, ForkID: e.ForkID, Kind: kind, Tool: d.Name, Args: string(d.Args), Ts: e.Ts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts > out[j].Ts })
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
	case "approve.request", "askuser.request":
		var d struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(e.Data, &d) == nil && d.ID != "" {
			return d.ID, true
		}
	}
	return "", false
}

/* ── Hub：设置、分支注册表与当前活动分支 ── */

/* Hub 管理应用级单例状态。Active 是当前分支；branches 按线根 ID 注册
存活分支（阶段一线间并发：后台分支的轮继续跑，事件进各自 turnFrames）。 */
type Hub struct {
	mu        sync.Mutex
	Models    ModelsConfig
	Fsys      osfs.OS
	Settings  Settings
	ToolRules []ToolRule
	Stats     *Stats
	Topics    *hooks.Topics
	Active    *Session
	branches  map[string]*Session
}

/* NewHub 创建领域根：加载配置记录（缺失文件自动创建默认）与累计生命体征，
迁移旧索引，并恢复活动会话。 */
func NewHub() *Hub {
	h := &Hub{Fsys: osfs.OS{}, branches: map[string]*Session{}}
	h.Models = ensureModelsConfig(h.Fsys)
	h.Settings = ensureSettings(h.Fsys)
	h.ToolRules = ensureToolRules(h.Fsys)
	h.Stats = NewStats(h.Fsys)
	migrateLegacyMemory(h.Fsys)
	h.Topics = hooks.NewTopics(h.Fsys)
	h.Topics.MigrateIndex(context.Background())
	h.Active = h.bootstrap()
	return h
}

/* SessionOf 按线根 ID 取存活分支（nil = 未加载）。 */
func (h *Hub) SessionOf(rootID string) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.branches[rootID]
}

/* Register 注册一个分支（按键 RootID）。 */
func (h *Hub) Register(s *Session) {
	if s == nil || s.RootID == "" {
		return
	}
	h.mu.Lock()
	h.branches[s.RootID] = s
	h.mu.Unlock()
}

/* Unregister 注销分支，返回是否活动分支被移除。 */
func (h *Hub) Unregister(rootID string) bool {
	h.mu.Lock()
	delete(h.branches, rootID)
	active := h.Active
	h.mu.Unlock()
	return active != nil && active.RootID == rootID
}

/* SetActive 切换当前分支（不取消旧分支运行中的轮）。 */
func (h *Hub) SetActive(s *Session) {
	h.mu.Lock()
	h.Active = s
	h.mu.Unlock()
}

/* Sessions 返回全部存活分支（Reassemble/Shutdown 遍历用）。 */
func (h *Hub) Sessions() []*Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*Session, 0, len(h.branches))
	for _, s := range h.branches {
		out = append(out, s)
	}
	return out
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

/* ensureToolRules 加载 toolRules.json，文件不存在则写盘默认。 */
func ensureToolRules(fsys osfs.OS) []ToolRule {
	if _, err := fsys.Read(context.Background(), "toolRules.json"); err != nil {
		rules := DefaultToolRules()
		_ = SaveToolRules(fsys, rules)
		return rules
	}
	return LoadToolRules(fsys)
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
		root := snap.LineRoot
		if root == "" {
			root = hooks.RootOf(ctx, h.Fsys, c.id) // 沿边上溯兜底（不依赖被回填的 LineRoot）
		}
		s := h.newSession(c.id, root, snap)
		s.restoreFrom(snap)
		h.Register(s)
		return s
	}
	s := h.newSession(hooks.NewSessionID(), "", nil)
	h.Register(s)
	return s
}

/* restoreFrom 按快照恢复会话内存态（历史/水位/资源基线/向上边）。 */
func (s *Session) restoreFrom(snap *hooks.SessionSnap) {
	s.setHistory(snap.Messages)
	s.Sess.SetTitle(snap.Title)
	s.Sess.SetResSnap(snap.Snapshot)
	s.Sess.SetLastOutputAt(snap.LastOutputAt)
	s.Sess.SeedUsage(snap.Usage)
	// 恢复水位（旧快照无字段时从历史最后一条 agent_status 兜底）
	if snap.CtxTokens > 0 {
		s.SetCtxTokens(snap.CtxTokens)
	} else {
		s.SetCtxTokens(hooks.LastCtxTokens(snap.Messages))
	}
	s.Sess.SetLineRoot(s.RootID)
	s.Sess.SetEdge(hooks.SnapEdge{
		TargetID: snap.TargetID, Anchor: snap.Anchor,
		SeedKind: snap.SeedKind, ForkedFrom: snap.ForkedFrom,
	})
	if snap.TargetID != "" && snap.SeedKind == "compress" {
		// 只有 compress 边进 prevID（fork 的 TargetID 是源会话，不是上翻链）
		s.Sess.SetPrev(snap.TargetID, snap.CompactSummary)
	}
}

/* PendingCount 返回未决人机请求数（分支面板"等待审批"指示用）。 */
func (s *Session) PendingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

/* CreateBranch 创建全新分支（空历史，自成一根）并注册。 */
func (h *Hub) CreateBranch() *Session {
	s := h.newSession(hooks.NewSessionID(), "", nil)
	h.Register(s)
	return s
}

/* LoadBranch 按快照恢复分支会话（rootID 显式指定，防快照 LineRoot 缺失）并注册。 */
func (h *Hub) LoadBranch(rootID string, snap *hooks.SessionSnap) *Session {
	s := h.newSession(snap.ID, rootID, snap)
	s.restoreFrom(snap)
	h.Register(s)
	return s
}

func (h *Hub) newSession(id string, rootID string, snap *hooks.SessionSnap) *Session {
	if rootID == "" {
		rootID = id // 新线：自成一根
	}
	s := &Session{
		ID:     id,
		RootID: rootID,
		Fsys:   h.Fsys,
		Sess:   hooks.NewStore(h.Fsys, id),
		Topics: h.Topics,
		snap:   snap,
		subs:   map[chan []byte]struct{}{},
		pending: map[string]Event{},
	}
	s.Sess.SetLineRoot(rootID)
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

/* RecordUsage 累计一轮主模型用量到启用条目并落盘（输入/输出/缓存分开记）。 */
func (h *Hub) RecordUsage(u *types.Usage) {
	if u == nil {
		return
	}
	h.mu.Lock()
	if e := h.Models.ActiveMain(); e != nil {
		accumulateUsage(e, u)
	}
	models := h.Models
	h.mu.Unlock()
	_ = SaveModelsConfig(h.Fsys, models)
}

/* RecordVisionUsage 累计图片识别（image_recognize）用量到识别槽启用条目并落盘。 */
func (h *Hub) RecordVisionUsage(u *types.Usage) {
	if u == nil {
		return
	}
	h.mu.Lock()
	for i := range h.Models.Vision {
		if h.Models.Vision[i].Enabled {
			accumulateUsage(&h.Models.Vision[i], u)
			break
		}
	}
	models := h.Models
	h.mu.Unlock()
	_ = SaveModelsConfig(h.Fsys, models)
}

/* accumulateUsage 把一份用量按输入/输出/缓存累进条目。 */
func accumulateUsage(e *ModelEntry, u *types.Usage) {
	e.InTokens += u.PromptTokens
	e.OutTokens += u.CompletionTokens
	e.CacheTokens += u.CachedTokens
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

/* ToolRulesSnapshot 返回当前审批策略快照。 */
func (h *Hub) ToolRulesSnapshot() []ToolRule {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ToolRules
}

/* ApplyToolRules 更新审批策略（持久化由 service 层完成）。 */
func (h *Hub) ApplyToolRules(rules []ToolRule) {
	h.mu.Lock()
	h.ToolRules = rules
	h.mu.Unlock()
}

/* event 包别名（供 service 引用统一出口）。 */
type E = event.Event
