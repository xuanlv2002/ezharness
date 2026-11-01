/*
Package domain 是 ezharness 的领域层：会话聚合（并发状态机）与事件帧。
零 HTTP 依赖——表现层（controller）只消费，不碰内部状态。
*/
package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/ext/hook/mcp"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/hooks"
	"ezharness/core/internal/osfs"
)

/* ErrBusy 表示会话当前有一轮运行未结束。 */
var ErrBusy = errors.New("session busy")

/* ErrSessionChanged 表示按会话 ID 发送时，该线已换代（compact）不再是同一会话。 */
var ErrSessionChanged = errors.New("session changed")

/* snapFlushBytes 是流式快照帧的重序列化增量阈值。 */
const snapFlushBytes = 8192

/* subBuffer 是单个 SSE 订阅者的帧缓冲。 */
const subBuffer = 1024

// Frame 是发往 SSE 订阅者的一帧。Type 用于慢消费者满缓冲时的丢弃决策：
// 高频增量帧可以丢，生命周期帧（tool_start/tool_end/model_end/turn_end…）
// 不能丢——丢了前端会一直停在「模型思考中」、工具卡不出现，直到下一条
// 无关事件碰巧到达。
type Frame struct {
	Type string
	Data []byte
}

/* droppable 判定高频增量帧：慢消费者积压时丢弃这些帧不影响时间线骨架。 */
func droppable(t string) bool {
	switch t {
	case "model_chunk", "reasoning_chunk", "tool_chunk", "tool.output":
		return true
	}
	return false
}

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
	PlanCh    chan<- hooks.PlanDecision
	McpAuthCh chan<- mcp.AuthDecision
	ToolNames []string
	Trace     *hooks.Trace // 调用链记录（Resume 切会话时同步切 trace）
}

/* ModelProvider 是领域所需的最小模型面（摘要用例），ezloop 同名接口的子集。 */
type ModelProvider interface {
	Invoke(ctx context.Context, req *types.ModelRequest) (*types.ModelResponse, error)
}

/*
	Session 是会话聚合：history、当前轮、SSE 订阅、未决请求。

ID = 当前叶 session ID（compact 换代随之更新）；RootID = 所属分支根
（稳定，注册表键与前端路由用它）。
*/
type Session struct {
	ID     string
	RootID string
	Fsys   osfs.OS
	Sess   *hooks.Store
	Topics *hooks.Topics

	mu          sync.Mutex
	history     []types.Message
	archiving   bool // 归档进行中（摘要最长 2 分钟）：锁发消息/切分支/二次归档
	cur         *runState
	subs        map[chan Frame]struct{}
	pending     map[string]Event
	completed   *PendingNotice // 最近一轮完成的 info 通知（新一轮覆盖旧值）
	ctxTokens   int
	wired       *Wiring
	snap        *hooks.SessionSnap         // bootstrap 恢复的快照（Assemble 读取；nil = 新建）
	sysP        *hooks.SysPrompt           // system 来源（Assemble 创建；resume/compact 热更）
	turnFrames  [][]byte                   // 本轮聚合帧缓存（刷新回放重建时间线；轮开始清空）
	turnRefs    []hooks.RefFile            // 本轮引用（StartRun 存入：loop_start 帧附带供回放重建 chips）
	snapAcc     map[string]*StreamSnapshot // forkID → in-flight 流累积快照（chunk 到达即累积）
	snapIdx     map[string]int             // forkID → 快照帧在 turnFrames 的下标
	snapFlushed map[string]int             // forkID → 上次序列化时的内容规模
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

/* setHistory 原子替换历史（测试/恢复会话用）。 */
func (s *Session) setHistory(msgs []types.Message) {
	s.mu.Lock()
	s.history = msgs
	s.mu.Unlock()
}

/*
ModelView 返回发给模型的历史：最后一个 <context_trim> marker（含）
之后的消息——marker 携带折叠段摘要，是折叠前后上下文的衔接点；
无 marker 时全量。归档摘要的输入用它（含 marker 摘要链）。
*/
func (s *Session) ModelView() []types.Message {
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

/*
	SetIdentity 同步会话标识（话题轮换/compact 后），水位清零

（恢复场景由调用方随后按快照重注）。RootID 不变：换代不换线。
*/
func (s *Session) SetIdentity(id string) {
	s.mu.Lock()
	s.ID = id
	s.ctxTokens = 0
	s.mu.Unlock()
}

/* CurrentID 返回当前叶 session ID（compact 换代随之变化）。 */
func (s *Session) CurrentID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ID
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
	return s.startRun("", ctx, text, refs)
}

/*
StartRunFor 在 StartRun 的基础上校验目标仍是同一个叶 session：调用方按
创建任务时保存的 sessionId 投递，compact 换代后拒绝而不是写到新会话。
*/
func (s *Session) StartRunFor(sessionID string, ctx context.Context, text string, refs []hooks.RefFile) (*core.RunHandle, context.CancelFunc, error) {
	return s.startRun(sessionID, ctx, text, refs)
}

func (s *Session) startRun(expectedID string, ctx context.Context, text string, refs []hooks.RefFile) (*core.RunHandle, context.CancelFunc, error) {
	s.mu.Lock()
	if expectedID != "" && s.ID != expectedID {
		s.mu.Unlock()
		return nil, nil, ErrSessionChanged
	}
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
	s.turnRefs = refs
	s.turnFrames = nil
	s.snapAcc = map[string]*StreamSnapshot{}
	s.snapIdx = map[string]int{}
	s.snapFlushed = map[string]int{}
	s.mu.Unlock()
	return h, cancel, nil
}

/* FinishRun 结束当前轮：更新历史、清未决请求、释放占用。 */
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
	s.turnRefs = nil
	s.pending = map[string]Event{}
	s.mu.Unlock()
}

/*
	Cancel 取消当前轮；无运行轮时补发一帧 turn_end（幂等纠正——轮在

SSE 断线窗口内结束时前端会错过 turn_end 而卡在"运行中"，取消操作
借此自愈）。
*/
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

/*
	Shutdown 收尾运行中的轮：取消并等待轮结束落盘（带超时，进程退出/

换代用——轮内历史只在 OnEnd 落盘，不等待直接退出会丢整轮）。
*/
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

/* DecidePlan 回传计划确认处置（confirm/feedback）。 */
func (s *Session) DecidePlan(d hooks.PlanDecision) {
	w := s.Wired()
	if w == nil {
		return
	}
	sendDecision(s, w.PlanCh, d, d.CallID)
}

/* DecideMcpAuth 回传 MCP 授权决策（授权完成后解除工具调用的阻塞）。 */
func (s *Session) DecideMcpAuth(d mcp.AuthDecision) {
	w := s.Wired()
	if w == nil {
		return
	}
	sendDecision(s, w.McpAuthCh, d, d.CallID)
}

/* ── SSE 订阅（领域事件出口，HTTP 帧写出在 controller）── */

// SubscribeWithReplay 原子地注册订阅并捕获回放快照：先取回放再订阅会
// 丢掉两步之间发布的事件（前端表现为一直停在「模型思考中」、工具卡不
// 出现，直到下一条无关事件）；先订阅再取回放又会把间隙事件重复发送。
// 锁内一次完成，回放帧在前、实时帧随后，无缝衔接。
func (s *Session) SubscribeWithReplay() (frames [][]byte, turnActive bool, ch <-chan Frame, unsub func()) {
	c := make(chan Frame, subBuffer)
	s.mu.Lock()
	s.subs[c] = struct{}{}
	turnActive = s.cur != nil
	frames = s.replayFramesLocked()
	s.mu.Unlock()
	return frames, turnActive, c, func() {
		s.mu.Lock()
		delete(s.subs, c)
		s.mu.Unlock()
	}
}

/*
	Publish 向全部订阅者扇出事件帧；人机请求登记 pending 供断线重放，

聚合帧缓存进 turnFrames 供整轮回放（刷新重建时间线）。主轮 loop_start
帧附带本轮引用（运行中切回分支时前端按回放重建 user 块的 chips——
本轮消息要等 FinishRun 才并入历史，getHistory 里没有）。
*/
func (s *Session) Publish(e Event) {
	if e.Type == "loop_start" && e.ForkID == "" && len(s.turnRefs) > 0 {
		// 载荷扩为 {text, files, fileRefs}：items 空 = 附件 chip，非空 = 引用 chip
		var text string
		_ = json.Unmarshal(e.Data, &text)
		type namedPath struct {
			Name string `json:"name"`
			Path string `json:"path"`
		}
		payload := struct {
			Text     string          `json:"text"`
			Files    []namedPath     `json:"files,omitempty"`
			FileRefs []hooks.RefFile `json:"fileRefs,omitempty"`
		}{Text: text}
		for _, r := range s.turnRefs {
			if len(r.Items) == 0 {
				payload.Files = append(payload.Files, namedPath{Name: filepath.Base(filepath.FromSlash(r.Path)), Path: r.Path})
			} else {
				payload.FileRefs = append(payload.FileRefs, r)
			}
		}
		if b, err := json.Marshal(payload); err == nil {
			e.Data = b
		}
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	frame := Frame{Type: e.Type, Data: data}
	s.mu.Lock()
	if e.Type == "approve.request" || e.Type == "askuser.request" ||
		e.Type == "plan.request" || e.Type == "mcp.auth.request" {
		var d struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(e.Data, &d) == nil && d.ID != "" {
			s.pending[d.ID] = e
		}
	}
	s.trackStreamLocked(e)
	if replayable(e.Type) {
		s.turnFrames = append(s.turnFrames, data)
	}
	for ch := range s.subs {
		if droppable(e.Type) {
			// 高频增量：缓冲满时直接丢弃（model_end 会带完整正文/思考，
			// stream.snapshot 供重连回放，丢增量不影响最终状态）
			select {
			case ch <- frame:
			default:
			}
			continue
		}
		// 生命周期帧必须送达：满缓冲时挤掉最旧一帧腾位。FIFO 头部
		// 通常是同一段流积压的增量帧，丢一帧增量远比丢 tool_start 安全。
		for {
			select {
			case ch <- frame:
			default:
				select {
				case <-ch:
				default:
				}
				continue
			}
			break
		}
	}
	s.mu.Unlock()
}

/*
trackStreamLocked 维护 in-flight 流的快照帧：增量帧（正文/思考/构造期
工具）到达即累积进快照并在 turnFrames 就地更新（一条流一帧，内存有界），
model_end 落定后丢弃（内容已被聚合帧覆盖），turn_end/新轮清场。只进
回放缓存，不发给实时订阅者（它们收增量帧）。
*/
func (s *Session) trackStreamLocked(e Event) {
	fork := e.ForkID
	switch e.Type {
	case "model_chunk", "reasoning_chunk":
		var delta string
		if json.Unmarshal(e.Data, &delta) != nil || delta == "" {
			return
		}
		acc := s.snapAcc[fork]
		if acc == nil {
			acc = &StreamSnapshot{}
			s.snapAcc[fork] = acc
		}
		if e.Type == "model_chunk" {
			acc.Content += delta
		} else {
			acc.Reasoning += delta
		}
	case "tool_chunk":
		var d struct {
			Index     int    `json:"index"`
			NameDelta string `json:"nameDelta"`
			ArgsDelta string `json:"argsDelta"`
		}
		if json.Unmarshal(e.Data, &d) != nil || (d.NameDelta == "" && d.ArgsDelta == "") {
			return
		}
		acc := s.snapAcc[fork]
		if acc == nil {
			acc = &StreamSnapshot{}
			s.snapAcc[fork] = acc
		}
		var bucket *SnapTool
		for i := range acc.Tools {
			if acc.Tools[i].Index == d.Index {
				bucket = &acc.Tools[i]
				break
			}
		}
		if bucket == nil {
			acc.Tools = append(acc.Tools, SnapTool{Index: d.Index})
			bucket = &acc.Tools[len(acc.Tools)-1]
		}
		bucket.Name += d.NameDelta
		bucket.Args += d.ArgsDelta
	case "model_end", "turn_end", "loop_start":
		s.dropSnapLocked(fork)
		return
	default:
		return
	}
	s.putSnapLocked(e, fork)
}

/* putSnapLocked 把当前快照写进 turnFrames（按增量节流序列化）。 */
func (s *Session) putSnapLocked(e Event, fork string) {
	acc := s.snapAcc[fork]
	if acc == nil || (acc.Content == "" && acc.Reasoning == "" && len(acc.Tools) == 0) {
		return
	}
	size := len(acc.Content) + len(acc.Reasoning)
	for _, t := range acc.Tools {
		size += len(t.Name) + len(t.Args)
	}
	if _, ok := s.snapIdx[fork]; ok && size-s.snapFlushed[fork] < snapFlushBytes {
		return // 已有帧且增量未到阈值：跳过本轮序列化
	}
	s.writeSnapLocked(e.Ts, fork, acc, size)
}

/* writeSnapLocked 序列化快照帧写入 turnFrames。 */
func (s *Session) writeSnapLocked(ts int64, fork string, acc *StreamSnapshot, size int) {
	frame := Event{Type: "stream.snapshot", Ts: ts, ForkID: fork, Data: Raw(*acc)}
	b, err := json.Marshal(frame)
	if err != nil {
		return
	}
	s.snapFlushed[fork] = size
	if i, ok := s.snapIdx[fork]; ok && i < len(s.turnFrames) {
		s.turnFrames[i] = b
		return
	}
	s.turnFrames = append(s.turnFrames, b)
	s.snapIdx[fork] = len(s.turnFrames) - 1
}

/* dropSnapLocked 丢弃该流的快照帧并修正其余下标。 */
func (s *Session) dropSnapLocked(fork string) {
	delete(s.snapAcc, fork)
	delete(s.snapFlushed, fork)
	i, ok := s.snapIdx[fork]
	if !ok {
		return
	}
	delete(s.snapIdx, fork)
	if i >= len(s.turnFrames) {
		return
	}
	s.turnFrames = append(s.turnFrames[:i], s.turnFrames[i+1:]...)
	for k, v := range s.snapIdx {
		if v > i {
			s.snapIdx[k] = v - 1
		}
	}
}

/*
	replayable 判定帧是否进回放缓存：聚合帧保留，高频增量与瞬态帧跳过

（resource.change 与 status.snapshot 同理：对应块插在 pendingUser 截断点
之前，回放重插会重复渲染）。
*/
func replayable(t string) bool {
	switch t {
	case "model_chunk", "reasoning_chunk", "tool_chunk", "model_start", "tool.output",
		"status.snapshot", "resource.change", "loop.guard", "turn_end", "loop_end", "iteration_end":
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

/* SnapTool 是流式快照里的构造期工具调用（累积量，非增量）。 */
type SnapTool struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Args  string `json:"args"`
}

/*
StreamSnapshot 是 in-flight 流的累积快照帧载荷：当前这次模型调用的
正文/思考/构造期工具调用全量。增量帧（model_chunk 等）不进回放缓存，
切回分支/断线重连时正在流式的内容只有这里可见——快照帧在 turnFrames
里就地更新（一条流一帧，内存有界），实时订阅者不收它（走增量）。
*/
type StreamSnapshot struct {
	Content   string     `json:"content,omitempty"`
	Reasoning string     `json:"reasoning,omitempty"`
	Tools     []SnapTool `json:"tools,omitempty"`
}

/* PendingNotice 是通知栏全局条目（GET /api/notifications 的域模型）。 */
type PendingNotice struct {
	CallID string `json:"callId"`
	ForkID string `json:"forkId,omitempty"`
	Kind   string `json:"kind"` // approve | ask | info
	Tool   string `json:"tool"`
	Args   string `json:"args,omitempty"`
	Ts     int64  `json:"ts"`
}

/*
MarkCompleted 记录本会话最近一轮的完成通知（info 条，与会话一一对应——
新一轮覆盖旧值，关闭后置空）。detail 是人读文案（结束原因 · 轮次 · 耗时）。
*/
func (s *Session) MarkCompleted(detail string) {
	now := time.Now().UnixMilli()
	args, _ := json.Marshal(map[string]string{"detail": detail})
	s.mu.Lock()
	s.completed = &PendingNotice{
		CallID: fmt.Sprintf("done-%d", now),
		Kind:   "info",
		Tool:   "执行完成",
		Args:   string(args),
		Ts:     now,
	}
	s.mu.Unlock()
}

/* CompletedNotice 返回最近一轮完成通知（无则 nil）。 */
func (s *Session) CompletedNotice() *PendingNotice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completed
}

/* DismissCompleted 关闭完成通知（仅 callID 匹配才清，避免误清新一轮）。 */
func (s *Session) DismissCompleted(callID string) {
	s.mu.Lock()
	if s.completed != nil && s.completed.CallID == callID {
		s.completed = nil
	}
	s.mu.Unlock()
}

/*
	PendingNotices 返回未决人机请求快照（通知栏跨分支轮询数据源）。

按请求时间降序（新在前）——pending 是 map，遍历序随机，固定排序保证
轮询结果稳定不抖动。
*/
func (s *Session) PendingNotices() []PendingNotice {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PendingNotice, 0, len(s.pending))
	for _, e := range s.pending {
		switch e.Type {
		case "approve.request", "askuser.request", "plan.request":
			kind := "approve"
			switch e.Type {
			case "askuser.request":
				kind = "ask"
			case "plan.request":
				kind = "plan"
			}
			var d ToolStartData
			if json.Unmarshal(e.Data, &d) != nil || d.ID == "" {
				continue
			}
			out = append(out, PendingNotice{CallID: d.ID, ForkID: e.ForkID, Kind: kind, Tool: d.Name, Args: string(d.Args), Ts: e.Ts})
		case "mcp.auth.request":
			var d struct {
				ID     string `json:"id"`
				Server string `json:"server"`
			}
			if json.Unmarshal(e.Data, &d) != nil || d.ID == "" {
				continue
			}
			out = append(out, PendingNotice{CallID: d.ID, ForkID: e.ForkID, Kind: "mcp_auth", Tool: d.Server, Ts: e.Ts})
		}
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
	return s.replayFramesLocked()
}

// replayFramesLocked 是 ReplayFrames 的无锁实现（调用方须持有 s.mu）：
// SubscribeWithReplay 在同一次持锁内取快照，避免与订阅注册之间出现
// 丢帧/重帧窗口。
func (s *Session) replayFramesLocked() [][]byte {
	if s.cur == nil {
		out := make([][]byte, 0, len(s.pending))
		for _, e := range s.pending {
			if data, err := json.Marshal(e); err == nil {
				out = append(out, data)
			}
		}
		return out
	}
	// 回放前把 in-flight 快照强制 flush 到最新（绕过节流）
	for fork, acc := range s.snapAcc {
		if acc == nil || (acc.Content == "" && acc.Reasoning == "" && len(acc.Tools) == 0) {
			continue
		}
		size := len(acc.Content) + len(acc.Reasoning)
		for _, t := range acc.Tools {
			size += len(t.Name) + len(t.Args)
		}
		if _, ok := s.snapIdx[fork]; !ok || size != s.snapFlushed[fork] {
			s.writeSnapLocked(time.Now().UnixMilli(), fork, acc, size)
		}
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

/* ── Hub：设置、分支注册表与当前活动分支 ── */

/*
Hub 管理应用级单例状态：active 是当前分支，branches 按线根 ID 注册存活分支。
*/
type Hub struct {
	mu        sync.Mutex
	Models    ModelsConfig
	Fsys      osfs.OS
	Settings  Settings
	ToolRules []ToolRule
	Stats     *Stats
	Topics    *hooks.Topics
	active    *Session
	branches  map[string]*Session
}

/* ActiveSession 返回当前活动分支。 */
func (h *Hub) ActiveSession() *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active
}

/* NewHub 创建领域根（空聚合；配置加载与恢复由 service.BootstrapHub 装配）。 */
func NewHub() *Hub {
	return &Hub{Fsys: osfs.OS{}, branches: map[string]*Session{}}
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
	active := h.active
	h.mu.Unlock()
	return active != nil && active.RootID == rootID
}

/* SetActive 切换当前分支（不取消旧分支运行中的轮）。 */
func (h *Hub) SetActive(s *Session) {
	h.mu.Lock()
	h.active = s
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

/* BootstrapActive 恢复最近未封存的存档，没有则新建。 */
func (h *Hub) BootstrapActive() *Session {
	if snap := hooks.LatestMainSnap(context.Background(), h.Fsys); snap != nil {
		s := h.newSession(snap.ID, snap.LineRoot, snap)
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
		ID:          id,
		RootID:      rootID,
		Fsys:        h.Fsys,
		Sess:        hooks.NewStore(h.Fsys, id),
		Topics:      h.Topics,
		snap:        snap,
		subs:        map[chan Frame]struct{}{},
		pending:     map[string]Event{},
		snapAcc:     map[string]*StreamSnapshot{},
		snapIdx:     map[string]int{},
		snapFlushed: map[string]int{},
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

/* ApplyUsage 累计主模型用量到启用条目，返回是否有变更。 */
func (h *Hub) ApplyUsage(u *types.Usage) bool {
	if u == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if e := h.Models.ActiveMain(); e != nil {
		accumulateUsage(e, u)
		return true
	}
	return false
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
