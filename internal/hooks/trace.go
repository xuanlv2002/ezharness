/*
trace 是调用链记录 hook：session 的每次 turn / 模型调用 / 工具调用 /
fork / compact 记为 otel 风格 span（traceId/spanId/parentId/时间戳/
耗时/attrs），落盘 sessions/<id>/trace.jsonl（fork 写 forks/ 子目录），
供调用链排查看板消费（瀑布图、慢工具 TopN、token 趋势）。

attrs 记输入输出的策略：新生成内容全记（turn 输入与最终回复、model
输出、tool args），累积内容不记（model 输入=session.json 已有），
超大内容截断（tool 结果 1KB、reasoning 2KB）。写入策略：内存累积 +
flush 全量重写（span 量百级，避免半行损坏）；模型调用结束即 flush，
崩溃最多丢当前迭代的未闭合 span。

并发：主循环与 fork 并行共享 hook 实例，按 *LoopState 分桶管理打开
的 span；OnToolStart/OnToolEnd 跨调用并发，全方法持锁。
*/
package hooks

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/xuanlv2002/ezloop/ext/fs"
	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/types"
)

/* Span 是一条调用链记录。 */
type Span struct {
	TraceID   string         `json:"traceId"`
	SpanID    string         `json:"spanId"`
	ParentID  string         `json:"parentId,omitempty"`
	Name      string         `json:"name"`
	Kind      string         `json:"kind"` // turn | model | tool | fork | compact
	Start     int64          `json:"start"`
	End       int64          `json:"end,omitempty"`
	DurMs     int64          `json:"durMs,omitempty"`
	Iteration int            `json:"iteration,omitempty"`
	ForkID    string         `json:"forkId,omitempty"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

/* bucket 是一个 Run（主循环或 fork）的打开 span 集。 */
type bucket struct {
	root  *Span
	model *Span
	tools map[string]*Span
}

/* Trace 实现全生命周期调用链记录。 */
type Trace struct {
	fsys   fs.FileSystem
	store  *Store
	modelF func() string

	mu      sync.Mutex
	id      string
	spans   []*Span
	buckets map[*types.LoopState]*bucket
}

/* NewTrace 创建记录器并续读当前会话已有的 trace.jsonl。 */
func NewTrace(fsys fs.FileSystem, store *Store, modelF func() string) *Trace {
	t := &Trace{fsys: fsys, store: store, modelF: modelF, buckets: map[*types.LoopState]*bucket{}}
	t.mu.Lock()
	t.id = store.ID()
	t.spans = t.loadLocked(t.id)
	t.mu.Unlock()
	return t
}

/* SetTrace 切换 trace（compact 创建新 session 时）：旧 trace 封存，新 trace 续读。 */
func (t *Trace) SetTrace(id string) {
	if id == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if id == t.id {
		return
	}
	t.flushLocked()
	t.id = id
	t.spans = t.loadLocked(id)
}

func (t *Trace) Name() string { return "trace" }

/* OnStart 打开主循环 turn 根 span。 */
func (t *Trace) OnStart(_ context.Context, state *types.LoopState) error {
	if state.ForkID != "" {
		return nil // fork 不重跑 startHooks，防御分支
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.bucketLocked(state)
	b.root = t.openLocked("turn", "turn", "", state, map[string]any{"input": state.Input})
	return nil
}

/* OnModelStart 打开模型调用 span（parent=turn/fork 根）。 */
func (t *Trace) OnModelStart(_ context.Context, state *types.LoopState) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.bucketLocked(state)
	b.model = t.openLocked("model", "model", b.root.SpanID, state, nil)
	return nil
}

/* OnModelEnd 关闭模型 span：记输出全文、reasoning 截断、tokens、工具清单。 */
func (t *Trace) OnModelEnd(_ context.Context, state *types.LoopState) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.bucketLocked(state)
	sp := b.model
	b.model = nil
	if sp == nil {
		return nil
	}
	attrs := map[string]any{}
	if r := state.LastResponse; r != nil {
		attrs["content"] = r.Content
		attrs["reasoning"] = truncStr(r.Reasoning, 2048)
		attrs["promptTokens"] = r.Usage.PromptTokens
		attrs["completionTokens"] = r.Usage.CompletionTokens
		attrs["cachedTokens"] = r.Usage.CachedTokens
		if t.modelF != nil {
			attrs["model"] = t.modelF()
		}
		if len(r.ToolCalls) > 0 {
			names := make([]string, 0, len(r.ToolCalls))
			for _, c := range r.ToolCalls {
				names = append(names, c.Name)
			}
			attrs["toolCalls"] = names
		}
	}
	t.closeLocked(sp, attrs)
	t.flushLocked()
	return nil
}

/* OnToolStart 打开工具 span（按 call.ID 配对，支持并发调用）。 */
func (t *Trace) OnToolStart(_ context.Context, state *types.LoopState, call *types.ToolCall) (ezhook.Action, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.bucketLocked(state)
	b.tools[call.ID] = t.openLocked("tool."+call.Name, "tool", b.root.SpanID, state, map[string]any{
		"callId": call.ID,
		"args":   truncStr(string(call.Args), 4096),
	})
	return ezhook.Proceed, nil
}

/* OnToolEnd 关闭工具 span：记结果截断与成败（offload 已改写则含路径）。 */
func (t *Trace) OnToolEnd(_ context.Context, state *types.LoopState, result *types.ToolResult) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.bucketLocked(state)
	sp := b.tools[result.CallID]
	delete(b.tools, result.CallID)
	if sp == nil {
		return nil
	}
	attrs := map[string]any{"content": truncStr(result.Content, 1024)}
	if result.Err != nil {
		attrs["err"] = result.Err.Error()
	} else {
		attrs["ok"] = true
	}
	t.closeLocked(sp, attrs)
	return nil
}

/* OnEnd 关闭根 span（turn/fork）：记最终回复、停止原因、迭代数，flush。 */
func (t *Trace) OnEnd(_ context.Context, state *types.LoopState) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buckets[state]
	if b == nil {
		return nil
	}
	delete(t.buckets, state)
	attrs := map[string]any{"stopReason": string(state.StopReason), "iterations": state.Iteration}
	if ans := lastAssistantText(state); ans != "" {
		attrs["answer"] = truncStr(ans, 8192)
	}
	t.closeLocked(b.root, attrs)
	t.closeLocked(b.model, nil) // 防御：错误路径残留的未闭合 span
	for _, sp := range b.tools {
		t.closeLocked(sp, nil)
	}
	t.flushLocked()
	return nil
}

/* StartSpan/EndSpan 是同步记录 API（compact hook 用：触发→摘要完成）。 */
func (t *Trace) StartSpan(state *types.LoopState, name, kind string, attrs map[string]any) *Span {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.openLocked(name, kind, "", state, attrs)
}

func (t *Trace) EndSpan(sp *Span, attrs map[string]any) {
	if sp == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closeLocked(sp, attrs)
	t.flushLocked()
}

/*
CloseRoot 提前关闭一个 Run 的根 span 并 flush（compact 换库前调用：
旧 trace 里 turn 必须闭合落盘，SetTrace 后旧 span 指针不再可达）。
*/
func (t *Trace) CloseRoot(state *types.LoopState, attrs map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buckets[state]
	if b == nil {
		return
	}
	delete(t.buckets, state)
	t.closeLocked(b.root, attrs)
	t.closeLocked(b.model, nil)
	for _, sp := range b.tools {
		t.closeLocked(sp, nil)
	}
	t.flushLocked()
}

/* OpenRoot 为已有 Run 重开根 span（compact 工具路径：新 trace 承接本轮剩余迭代）。 */
func (t *Trace) OpenRoot(state *types.LoopState, attrs map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.buckets[state]; ok {
		return
	}
	t.bucketLocked(state).root = t.openLocked(
		map[bool]string{true: "fork." + state.ForkID, false: "turn"}[state.ForkID != ""],
		map[bool]string{true: "fork", false: "turn"}[state.ForkID != ""],
		"", state, attrs)
}

/* ── 内部（全持 t.mu）── */

/* bucketLocked 取或建分桶；fork 首次触达时惰性开根 span。 */
func (t *Trace) bucketLocked(state *types.LoopState) *bucket {
	b, ok := t.buckets[state]
	if !ok {
		b = &bucket{tools: map[string]*Span{}}
		t.buckets[state] = b
		if state.ForkID != "" {
			b.root = t.openLocked("fork."+state.ForkID, "fork", "", state,
				map[string]any{"task": state.Input})
		}
	}
	if b.root == nil {
		// 主循环 OnStart 之前的回调（理论不可达）兜底开根
		b.root = t.openLocked("turn", "turn", "", state, nil)
	}
	return b
}

func (t *Trace) openLocked(name, kind, parent string, state *types.LoopState, attrs map[string]any) *Span {
	sp := &Span{
		TraceID:   t.id,
		SpanID:    newSpanID(),
		ParentID:  parent,
		Name:      name,
		Kind:      kind,
		Start:     time.Now().UnixMilli(),
		Iteration: state.Iteration,
		ForkID:    state.ForkID,
		Attrs:     attrs,
	}
	t.spans = append(t.spans, sp)
	return sp
}

func (t *Trace) closeLocked(sp *Span, attrs map[string]any) {
	if sp == nil {
		return
	}
	sp.End = time.Now().UnixMilli()
	sp.DurMs = sp.End - sp.Start
	for k, v := range attrs {
		if sp.Attrs == nil {
			sp.Attrs = map[string]any{}
		}
		sp.Attrs[k] = v
	}
}

/* flushLocked 全量重写：主 trace 与各 fork trace 分流落盘。 */
func (t *Trace) flushLocked() {
	if len(t.spans) == 0 {
		return
	}
	var main bytes.Buffer
	forks := map[string]*bytes.Buffer{}
	for _, sp := range t.spans {
		b, err := json.Marshal(sp)
		if err != nil {
			continue
		}
		if sp.ForkID == "" {
			main.Write(b)
			main.WriteByte('\n')
		} else {
			fb := forks[sp.ForkID]
			if fb == nil {
				fb = &bytes.Buffer{}
				forks[sp.ForkID] = fb
			}
			fb.Write(b)
			fb.WriteByte('\n')
		}
	}
	ctx := context.Background()
	if main.Len() > 0 {
		_ = t.fsys.Write(ctx, SessionsDir+"/"+t.id+"/trace.jsonl", main.Bytes())
	}
	for fid, fb := range forks {
		_ = t.fsys.Write(ctx, SessionsDir+"/"+t.id+"/forks/"+fid+"/trace.jsonl", fb.Bytes())
	}
}

/* loadLocked 续读已有 trace（旧 span 只读保序，新 span 追加）。 */
func (t *Trace) loadLocked(id string) []*Span {
	data, err := t.fsys.Read(context.Background(), SessionsDir+"/"+id+"/trace.jsonl")
	if err != nil {
		return nil
	}
	var out []*Span
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var sp Span
		if json.Unmarshal(line, &sp) == nil {
			out = append(out, &sp)
		}
	}
	return out
}

/* lastAssistantText 取最后一条 assistant 文本。 */
func lastAssistantText(state *types.LoopState) string {
	for i := len(state.Messages) - 1; i >= 0; i-- {
		if state.Messages[i].Role == types.RoleAssistant {
			return state.Messages[i].Content
		}
	}
	return ""
}

/* truncStr 按 rune 截断（超出标记总长）。 */
func truncStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…（共 " + strconv.Itoa(len(r)) + " 字符已截断）"
}

func newSpanID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
