/*
sessionstore 将会话以文件夹布局持久化：sessions/<id>/session.json 是
可恢复快照（消息历史、固定 systemPrompt、工具清单、compact 链引用），
fork 子循环写 sessions/<主ID>/forks/<forkID>/session.json（剥离 seed
只存增量）。ListMain 只认目录项——fork 归属主会话子目录，天然不混入
恢复候选。
*/
package hooks

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/task"
	"github.com/xuanlv2002/ezloop/types"
)

/* SessionsDir 是会话根目录（工作目录相对）。 */
const SessionsDir = "sessions"

/* ResSnapshot 是 agent_status 的变更对比快照（skill/mcp/终端清单基线）。 */
type ResSnapshot struct {
	Skills []string `json:"skills,omitempty"`
	Mcps   []string `json:"mcps,omitempty"`
	Terms  []string `json:"terms,omitempty"` // 终端基线（"id|名称|是否退出" 编码）
}

/* ForkOrigin 是 fork 线的展示元数据（时间线渲染"分叉自 X"，非结构依赖）。 */
type ForkOrigin struct {
	SourceID string `json:"sourceId,omitempty"`
	Title    string `json:"title,omitempty"` // 源 session 名称（记忆页"来自 X"展示；非分支名）
	Anchor   int    `json:"anchor,omitempty"`
}

/* SnapEdge 是会话的向上引用边（创建时写死，不可变）。 */
type SnapEdge struct {
	TargetID   string      // 引用目标（""=空根）
	Anchor     int         // fork：复制的消息前缀长度（含选中消息）
	SeedKind   string      // new | fork | compress
	ForkedFrom *ForkOrigin // fork 线的出处标签
}

/* SessionSnap 是一次会话的可持久化快照。 */
type SessionSnap struct {
	ID             string          `json:"id"`
	CreatedAt      int64           `json:"createdAt"`
	Title          string          `json:"title,omitempty"` // session 自己的名称（本代首条 user；与线标题独立）
	Input          string          `json:"input,omitempty"`
	Messages       []types.Message `json:"messages"`               // 剥离 system（systemPrompt 单独 pin）
	SystemPrompt   string          `json:"systemPrompt"`           // 渲染后完整 system（恢复零逻辑）
	SystemBase     string          `json:"systemBase"`             // 基础段（人格+记忆+skill/mcp 列表）
	SummaryBlock   string          `json:"summaryBlock,omitempty"` // compact 摘要段
	Tools          []string        `json:"tools,omitempty"`
	Model          string          `json:"model,omitempty"`
	CompactSummary string          `json:"compactSummary,omitempty"` // 上一 session 的摘要
	TargetID       string          `json:"targetId,omitempty"`       // 向上边目标（""=空根）
	Anchor         int             `json:"anchor,omitempty"`         // fork：复制的消息前缀长度
	SeedKind       string          `json:"seedKind,omitempty"`       // new | fork | compress
	LineRoot       string          `json:"lineRoot,omitempty"`       // 所属分支根 ID（冗余，链操作 O(1)）
	ForkedFrom     *ForkOrigin     `json:"forkedFrom,omitempty"`     // 分叉出处（展示元数据）
	LastOutputAt   int64           `json:"lastOutputAt,omitempty"`   // agent_status 距上次输出用
	Snapshot       *ResSnapshot    `json:"snapshot,omitempty"`       // 资源清单快照（nil = 基线未建）
	Usage          types.Usage     `json:"usage"`                    // 本会话累计用量（状态卡展示）
	CtxTokens      int             `json:"ctxTokens,omitempty"`      // 最近一次模型调用的上下文水位
	CtxWindow      int             `json:"ctxWindow,omitempty"`      // 主模型上下文窗口
	Iterations     int             `json:"iterations"`
	StopReason     string          `json:"stopReason"`
	StartedAt      time.Time       `json:"startedAt"`
	EndedAt        time.Time       `json:"endedAt"`
	Archived       bool            `json:"archived"` // compact 后旧库封存，不再作恢复候选
}

/* Store 实现 EndHook：每轮结束落盘快照，SetID 切换会话。 */
type Store struct {
	fsys     fs.FileSystem
	mu       sync.Mutex
	id       string
	title    string     // session 自己的名称（与线标题独立：线=分支身份，session=世代名）
	sys      *SysPrompt // system 唯一来源，OnEnd 取值 pin 进快照
	tool     string     // 主模型名（宿主注入）
	snap     *ResSnapshot
	last     int64                       // lastOutputAt（status hook 维护）
	prevID   string                      // compact 链：上一 session ID（SetPrev 设置）
	prevSum  string                      // compact 链：上一 session 摘要
	edge     SnapEdge                    // 当前会话向上边（恢复/fork 时注入，OnEnd 落盘）
	lineRoot string                      // 所属分支根 ID（SetID 不清：compact 换代不换线）
	usage    types.Usage                 // 本会话累计用量（OnEnd 累计并随快照落盘）
	runID    string                      // 本轮开始时的会话 ID（compact 轮内切库时拒绝把用量记入新库）
	ctx      func() (tokens, window int) // 上下文水位与窗口（宿主注入，快照落盘用）
}

/* NewStore 创建存储 hook。id 为空自动生成。 */
func NewStore(fsys fs.FileSystem, id string) *Store {
	if id == "" {
		id = NewSessionID()
	}
	return &Store{fsys: fsys, id: id}
}

/* BindSys 绑定 system 来源与模型名（Assemble 时注入）。 */
func (h *Store) BindSys(sys *SysPrompt, model string) {
	h.mu.Lock()
	h.sys, h.tool = sys, model
	h.mu.Unlock()
}

/* ID 返回当前会话 ID。 */
func (h *Store) ID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.id
}

/* SetID 切换会话（后续轮次写入新 ID 的文件夹）。lineRoot 不清：compact 换代不换线。 */
func (h *Store) SetID(id string) {
	if id == "" {
		return
	}
	h.mu.Lock()
	if id != h.id {
		h.id = id
		h.snap = nil // 新会话资源基线由 status hook 重建
		h.last = 0
		h.prevID, h.prevSum = "", ""
		h.edge = SnapEdge{}
		h.usage = types.Usage{} // 新会话用量重新累计
		h.title = ""            // 新代未命名起步，落盘时按本代首条 user 命名
	}
	h.mu.Unlock()
}

/* SetLineRoot 记录所属分支根 ID（会话创建/恢复时注入）。 */
func (h *Store) SetLineRoot(root string) {
	h.mu.Lock()
	if root != "" {
		h.lineRoot = root
	}
	h.mu.Unlock()
}

/* SetTitle 设置 session 名称（恢复快照时注入；fork 创建时锚点名）。 */
func (h *Store) SetTitle(title string) {
	h.mu.Lock()
	h.title = title
	h.mu.Unlock()
}

/* Title 返回 session 名称（空 = 未命名，落盘时按本代首条 user 命名）。 */
func (h *Store) Title() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.title
}

/* LineRoot 返回所属分支根 ID（空 = 未知，调用方兜底）。 */
func (h *Store) LineRoot() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lineRoot
}

/* SetEdge 注入当前会话的向上边（恢复/fork 会话时；SetID 后调用）。 */
func (h *Store) SetEdge(e SnapEdge) {
	h.mu.Lock()
	h.edge = e
	h.mu.Unlock()
}

/* Edge 返回当前会话的向上边。 */
func (h *Store) Edge() SnapEdge {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.edge
}

/* BindCtx 注入上下文水位与窗口（Assemble 时调用）。 */
func (h *Store) BindCtx(fn func() (tokens, window int)) {
	h.mu.Lock()
	h.ctx = fn
	h.mu.Unlock()
}

/* CtxInfo 返回上下文水位与窗口（未绑定时 0,0）。 */
func (h *Store) CtxInfo() (int, int) {
	h.mu.Lock()
	fn := h.ctx
	h.mu.Unlock()
	if fn == nil {
		return 0, 0
	}
	return fn()
}

/* Usage 返回本会话累计用量副本。 */
func (h *Store) Usage() types.Usage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.usage
}

/* SeedUsage 恢复快照时注入累计用量（SetID 之后调用）。 */
func (h *Store) SeedUsage(u types.Usage) {
	h.mu.Lock()
	h.usage = u
	h.mu.Unlock()
}

/* ResSnap 返回资源清单快照（nil = 基线未建）。 */
func (h *Store) ResSnap() *ResSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snap
}

/* SetResSnap 更新资源清单快照。 */
func (h *Store) SetResSnap(s *ResSnapshot) {
	h.mu.Lock()
	h.snap = s
	h.mu.Unlock()
}

/* SetPrev 记录 compact 链引用（compact hook 在切新会话时调用）。 */
func (h *Store) SetPrev(oldID, summary string) {
	h.mu.Lock()
	h.prevID, h.prevSum = oldID, summary
	h.mu.Unlock()
}

/* PrevID 返回 compact 链上一会话 ID（空 = 本会话非压缩产物）。 */
func (h *Store) PrevID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.prevID
}

/* LastOutputAt 返回最近一次输出时间戳（毫秒，0 未知）。 */
func (h *Store) LastOutputAt() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}

/* SetLastOutputAt 更新最近输出时间。 */
func (h *Store) SetLastOutputAt(t int64) {
	h.mu.Lock()
	h.last = t
	h.mu.Unlock()
}

func (h *Store) Name() string { return "sessionstore" }

/* OnStart 记录本轮所属会话（compact 在 OnEnd 轮内切库时，用量仍归旧库口径——不记入新库）。 */
func (h *Store) OnStart(_ context.Context, _ *types.LoopState) error {
	h.mu.Lock()
	h.runID = h.id
	h.mu.Unlock()
	return nil
}

/*
OnEnd 持久化快照；失败不阻断主流程（错误记入 Metadata）。
fork 写主会话 forks/ 子目录，SeedLen 越界 clamp 全存（fork 内 compact
就地截断后 SeedLen 语义重置，剥离逻辑不得越界崩溃）。
*/
func (h *Store) OnEnd(ctx context.Context, state *types.LoopState) error {
	h.mu.Lock()
	id := h.id
	snap, sys, tool := h.snap, h.sys, h.tool
	last, prevID, prevSum := h.last, h.prevID, h.prevSum
	edge, lineRoot, title := h.edge, h.lineRoot, h.title
	h.mu.Unlock()

	fork := state.ForkID != ""
	// trim 追加式档案：全量 = 盘上已有（上轮末）MergeFull 本轮折叠段与当前消息
	// ——跨轮覆盖不丢早期档案（marker 前的部分从未进过本轮视图）
	full := state.Messages
	target := id
	if fork {
		target = state.ForkID
	}
	if old, err := LoadSnap(ctx, h.fsys, target); err == nil {
		full = MergeFull(old.Messages, state)
	} else if folded := FoldedOf(state); len(folded) > 0 {
		full = MergeFull(nil, state)
	}
	msgs := stripSystem(full)
	if fork && state.SeedLen > 0 && state.SeedLen <= len(msgs) {
		// SeedLen 含 system（fork.go 语义），stripSystem 后数组少 1：
		// 起点 -1 才不会把 seed 后首条（任务 input / trim marker）剥掉
		msgs = msgs[state.SeedLen-1:]
	}

	// session 名称：已命名沿用；未命名（归档新代起步）按本代首条真实
	// user 命名（FirstUserTitle 跳过 marker/end_reason/agent_status 注入）
	if title == "" && !fork {
		if t := FirstUserTitle(msgs); t != "未命名话题" {
			title = t
		}
	}
	if title != "" {
		h.mu.Lock()
		h.title = title // 命名后固定，后续落盘沿用
		h.mu.Unlock()
	}

	out := SessionSnap{
		ID:           id,
		CreatedAt:    state.StartedAt.UnixMilli(),
		Title:        title,
		Input:        state.Input,
		Messages:     msgs,
		Tools:        toolNames(state),
		Iterations:   state.Iteration,
		StopReason:   string(state.StopReason),
		StartedAt:    state.StartedAt,
		EndedAt:      state.EndedAt,
		LastOutputAt: last,
		Snapshot:     snap,
		LineRoot:     lineRoot,
	}
	if !fork && h.runID == id { // 主循环且本轮未切库：累计用量与水位（fork 只存增量；compact 切库轮的用量不计入新库）
		h.mu.Lock()
		h.usage.Add(state.Usage)
		usage := h.usage
		h.mu.Unlock()
		out.Usage = usage
		out.CtxTokens, out.CtxWindow = h.CtxInfo()
	}
	if sys != nil {
		base, summary := sys.Parts()
		out.SystemPrompt, out.SystemBase, out.SummaryBlock = sys.Prompt(), base, summary
	}
	if tool != "" {
		out.Model = tool
	}
	if !fork {
		// 向上边：compact 的 SetPrev 是一种边（目标=旧库，seed=compress），
		// 优先于恢复时注入的 edge（两者在 compress 恢复场景下同值）。
		if prevID != "" {
			edge = SnapEdge{TargetID: prevID, SeedKind: "compress"}
		}
		if edge.SeedKind == "" {
			edge.SeedKind = "new"
		}
		out.TargetID, out.Anchor, out.SeedKind, out.ForkedFrom = edge.TargetID, edge.Anchor, edge.SeedKind, edge.ForkedFrom
		if edge.SeedKind == "compress" && edge.TargetID != "" {
			out.CompactSummary = prevSum
		}
	}
	if fork {
		out.ID = state.ForkID
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		state.Metadata["sessionstore_error"] = err.Error()
		return nil
	}
	path := SessionsDir + "/" + id + "/session.json"
	if fork {
		path = SessionsDir + "/" + id + "/forks/" + state.ForkID + "/session.json"
	}
	if err := h.fsys.Write(context.Background(), path, data); err != nil {
		state.Metadata["sessionstore_error"] = err.Error()
	}
	return nil
}

/* toolNames 从注册表提取本轮全部工具名。 */
func toolNames(state *types.LoopState) []string {
	tools := state.Tools.List()
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name())
	}
	return out
}

/* stripSystem 剥离 system 消息（system 由 SysPrompt 单独 pin）。 */
func stripSystem(msgs []types.Message) []types.Message {
	out := make([]types.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role != types.RoleSystem {
			out = append(out, m)
		}
	}
	return out
}

/* NewSessionID 生成短随机会话 ID。 */
func NewSessionID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

/* DecisionRecord 是一条已回传的人机决策（审批/回答/规划处置）持久化记录。 */
type DecisionRecord struct {
	CallID     string `json:"callId"`
	Kind       string `json:"kind"` // approve | ask | plan
	Resolution string `json:"resolution"`
	Ts         int64  `json:"ts"`
}

/* AppendDecision 追加决策记录（sessions/<id>/decisions.jsonl，失败静默）。 */
func AppendDecision(ctx context.Context, fsys fs.FileSystem, id string, r DecisionRecord) {
	if r.CallID == "" || id == "" {
		return
	}
	data, _ := json.Marshal(r)
	prev, _ := fsys.Read(ctx, SessionsDir+"/"+id+"/decisions.jsonl")
	_ = fsys.Write(ctx, SessionsDir+"/"+id+"/decisions.jsonl", append(prev, append(data, '\n')...))
}

/* LoadDecisions 读回会话的全部决策记录（无文件返回 nil）。 */
func LoadDecisions(ctx context.Context, fsys fs.FileSystem, id string) []DecisionRecord {
	data, err := fsys.Read(ctx, SessionsDir+"/"+id+"/decisions.jsonl")
	if err != nil {
		return nil
	}
	var out []DecisionRecord
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r DecisionRecord
		if json.Unmarshal(line, &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

/* LoadSnap 读取指定会话快照（sessions/<id>/session.json）。 */
func LoadSnap(ctx context.Context, fsys fs.FileSystem, id string) (*SessionSnap, error) {
	data, err := fsys.Read(ctx, SessionsDir+"/"+id+"/session.json")
	if err != nil {
		return nil, fmt.Errorf("sessionstore: load %s: %w", id, err)
	}
	var s SessionSnap
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("sessionstore: decode %s: %w", id, err)
	}
	return &s, nil
}

/* SaveSnap 写回会话快照（sessions/<snap.ID>/session.json，失败返回 error）。 */
func SaveSnap(ctx context.Context, fsys fs.FileSystem, s *SessionSnap) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(ctx, SessionsDir+"/"+s.ID+"/session.json", data)
}

/* ForkSummary 是 fork 分身的列表摘要（历史重建 fork 卡片用）。 */
type ForkSummary struct {
	ID         string `json:"id"`
	Task       string `json:"task"`                 // 剥掉包装前缀的任务描述
	Answer     string `json:"answer,omitempty"`     // 最终回答
	StopReason string `json:"stopReason,omitempty"` // 空 = 正常完成
	Iterations int    `json:"iterations"`
}

/*
ListForks 返回主会话全部 fork 摘要，按 forkID 序号升序（与主库
task 调用顺序一致，前端据此把卡片插到对应工具块之后）。
存档均为已结束的分身（OnEnd 落盘），运行中的分身由实时事件重建。
*/
func ListForks(ctx context.Context, fsys fs.FileSystem, id string) []ForkSummary {
	entries, err := fsys.List(ctx, SessionsDir+"/"+id+"/forks")
	if err != nil || len(entries) == 0 {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir {
			names = append(names, e.Name)
		}
	}
	sort.Strings(names)
	out := make([]ForkSummary, 0, len(names))
	for _, name := range names {
		snap, err := LoadFork(ctx, fsys, id, name)
		if err != nil {
			continue
		}
		out = append(out, ForkSummary{
			ID:         snap.ID,
			Task:       strings.TrimPrefix(snap.Input, task.TaskInputPrefix),
			Answer:     snapLastAssistant(snap.Messages),
			StopReason: snap.StopReason,
			Iterations: snap.Iterations,
		})
	}
	return out
}

/* LoadFork 读取 fork 分身快照（sessions/<id>/forks/<fid>/session.json）。 */
func LoadFork(ctx context.Context, fsys fs.FileSystem, id, fid string) (*SessionSnap, error) {
	if fid == "" || strings.Contains(fid, "/") || strings.Contains(fid, "\\") || strings.Contains(fid, "..") {
		return nil, fmt.Errorf("sessionstore: bad fork id %q", fid)
	}
	data, err := fsys.Read(ctx, SessionsDir+"/"+id+"/forks/"+fid+"/session.json")
	if err != nil {
		return nil, fmt.Errorf("sessionstore: load fork %s/%s: %w", id, fid, err)
	}
	var s SessionSnap
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("sessionstore: decode fork %s/%s: %w", id, fid, err)
	}
	return &s, nil
}

/* snapLastAssistant 取最后一条 assistant 消息正文。 */
func snapLastAssistant(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == types.RoleAssistant {
			return msgs[i].Content
		}
	}
	return ""
}

/* ListMain 返回主会话 ID 清单（只认 sessions/ 下的目录项）。 */
func ListMain(ctx context.Context, fsys fs.FileSystem) ([]string, error) {
	entries, err := fsys.List(ctx, SessionsDir)
	if err != nil {
		return nil, nil // 目录不存在视为无会话
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir && !strings.Contains(e.Name, "-task-") {
			ids = append(ids, e.Name)
		}
	}
	return ids, nil
}
