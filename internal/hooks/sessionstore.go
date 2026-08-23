/*
sessionstore 将会话以文件夹布局持久化：sessions/<id>/session.json 是
可恢复快照（消息历史、固定 systemPrompt、工具清单、compact 链引用），
fork 子循环写 sessions/<主ID>/forks/<forkID>/session.json（剥离 seed
只存增量）。旧版平铺 sessions/<id>.json 不迁移不读取，ListMain 只认
目录项——fork 归属主会话子目录，天然不混入恢复候选。
*/
package hooks

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/types"
)

/* SessionsDir 是会话根目录（工作目录相对）。 */
const SessionsDir = "sessions"

/* ResSnapshot 是 agent_status 的变更对比快照（skill/mcp 资源清单）。 */
type ResSnapshot struct {
	Skills []string `json:"skills,omitempty"`
	Mcps   []string `json:"mcps,omitempty"`
}

/* SessionSnap 是一次会话的可持久化快照。 */
type SessionSnap struct {
	ID             string          `json:"id"`
	CreatedAt      int64           `json:"createdAt"`
	Input          string          `json:"input,omitempty"`
	Messages       []types.Message `json:"messages"` // 剥离 system（systemPrompt 单独 pin）
	SystemPrompt   string          `json:"systemPrompt"`           // 渲染后完整 system（恢复零逻辑）
	SystemBase     string          `json:"systemBase"`             // 基础段（人格+记忆+skill/mcp 列表）
	SummaryBlock   string          `json:"summaryBlock,omitempty"` // compact 摘要段
	Tools          []string        `json:"tools,omitempty"`
	Model          string          `json:"model,omitempty"`
	PrevSession    string          `json:"prevSession,omitempty"`    // compact 链：上一 session ID
	CompactSummary string          `json:"compactSummary,omitempty"` // 上一 session 的摘要
	LastOutputAt   int64           `json:"lastOutputAt,omitempty"`   // agent_status 距上次输出用
	Snapshot       *ResSnapshot    `json:"snapshot,omitempty"`       // 资源清单快照（nil = 基线未建）
	Iterations     int             `json:"iterations"`
	StopReason     string          `json:"stopReason"`
	StartedAt      time.Time       `json:"startedAt"`
	EndedAt        time.Time       `json:"endedAt"`
	Archived       bool            `json:"archived"` // compact 后旧库封存，不再作恢复候选
}

/* Store 实现 EndHook：每轮结束落盘快照，SetID 切换会话。 */
type Store struct {
	fsys fs.FileSystem
	mu   sync.Mutex
	id   string
	sys  *SysPrompt // system 唯一来源，OnEnd 取值 pin 进快照
	tool string     // 主模型名（宿主注入）
	snap *ResSnapshot
	last int64 // lastOutputAt（status hook 维护）
	prevID string // compact 链：上一 session ID
	prevSum string // compact 链：上一 session 摘要
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

/* SetID 切换会话（后续轮次写入新 ID 的文件夹）。 */
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
	}
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

/*
OnEnd 持久化快照；失败不阻断主流程（错误记入 Metadata）。
fork 写主会话 forks/ 子目录，SeedLen 越界 clamp 全存（fork 内 compact
就地截断后 SeedLen 语义重置，剥离逻辑不得越界崩溃）。
*/
func (h *Store) OnEnd(_ context.Context, state *types.LoopState) error {
	h.mu.Lock()
	id := h.id
	snap, sys, tool := h.snap, h.sys, h.tool
	last, prevID, prevSum := h.last, h.prevID, h.prevSum
	h.mu.Unlock()

	fork := state.ForkID != ""
	msgs := stripSystem(state.Messages)
	if fork && state.SeedLen > 0 && state.SeedLen <= len(msgs) {
		msgs = msgs[state.SeedLen:]
	}

	out := SessionSnap{
		ID:           id,
		CreatedAt:    state.StartedAt.UnixMilli(),
		Input:        state.Input,
		Messages:     msgs,
		Tools:        toolNames(state),
		Iterations:   state.Iteration,
		StopReason:   string(state.StopReason),
		StartedAt:    state.StartedAt,
		EndedAt:      state.EndedAt,
		LastOutputAt: last,
		Snapshot:     snap,
	}
	if sys != nil {
		base, summary := sys.Parts()
		out.SystemPrompt, out.SystemBase, out.SummaryBlock = sys.Prompt(), base, summary
	}
	if tool != "" {
		out.Model = tool
	}
	if prevID != "" {
		out.PrevSession, out.CompactSummary = prevID, prevSum
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

/* ListMain 返回主会话 ID 清单（只认 sessions/ 下的目录项，忽略旧平铺文件）。 */
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
