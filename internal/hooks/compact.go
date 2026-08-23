/*
compact 是上下文压缩 hook：模型主动调 compact_context 工具、或轮末
水位超阈值（prompt tokens > threshold）时自动触发。主循环路径：
总结当前 session → 重组 system（base 重读记忆/skill/mcp + 摘要段 +
上一 session 路径引用）→ 截断上下文供本轮剩余迭代使用；压缩轮的
完整历史（含触发输入与工具过程）在 OnEnd 收尾时补写进旧库封存，
此刻才切换新库并跳过新库落盘——新库空置起步，下一条记录是用户的
下一轮输入。fork 路径：就地压缩增量（不换库不归档，摘要拼进 fork
内的 system）。

对用户 session 完全透明，是 agent 的"记忆翻页"机制。
*/
package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/fs"
	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
)

/* CompactTool 是主动压缩工具的注册名。 */
const CompactTool = "compact_context"

/* EventCompact 是上下文压缩事件（主循环专属，Data 为 CompactInfo）。 */
const EventCompact = event.EventType("session.compact")

/* CompactInfo 描述一次压缩。 */
type CompactInfo struct {
	OldID    string `json:"oldId"`
	NewID    string `json:"newId"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Auto     bool   `json:"auto"`
	Reason   string `json:"reason,omitempty"`
	PrevPath string `json:"prevPath"`
}

const compactSummaryPrompt = "为切换到全新会话生成交接摘要：保留上一会话的关键事实、" +
	"已达成的决定、未完成的待办与用户偏好；忽略过程细节与状态栏记录；简洁自包含，300 字以内。"

/* Compact 提供模型自主与水位自动两条压缩路径。 */
type Compact struct {
	provider    provider.ModelProvider
	fsys        fs.FileSystem
	sess        *Store
	sys         *SysPrompt
	topics      *Topics
	trace       *Trace
	threshold   int // 水位阈值（prompt tokens），<=0 禁用自动压缩
	rebuildBase func() string
	onCompact   func(CompactInfo) // 压缩成功回调（宿主同步 session 标识）
	pending     *pendingCompact   // 挂起的压缩（轮末 OnEnd 收尾）
}

/*
pendingCompact 是一次挂起的压缩：工具路径截断后本轮还有剩余迭代
（模型汇总压缩结果），这些过程消息逻辑上属于旧库，故切库动作延迟
到 OnEnd——收尾时旧库补写完整历史并封存，新库空置起步。
*/
type pendingCompact struct {
	oldID, newID           string
	title, reason, summary string
	auto                   bool
	oldMsgs                []types.Message // 截断前全量（含本轮触发输入与 tool_calls）
	oldPrompt              string          // 压缩前的 system 三段（旧库存档 pin 用）
	oldBase, oldSummary    string
}

/* NewCompact 创建压缩 hook。rebuildBase/onCompact 可为 nil。 */
func NewCompact(p provider.ModelProvider, fsys fs.FileSystem, sess *Store, sys *SysPrompt,
	topics *Topics, trace *Trace, threshold int, rebuildBase func() string,
	onCompact func(CompactInfo)) *Compact {
	return &Compact{provider: p, fsys: fsys, sess: sess, sys: sys, topics: topics,
		trace: trace, threshold: threshold, rebuildBase: rebuildBase, onCompact: onCompact}
}

func (c *Compact) Name() string { return "compact" }

/* OnStart 注册 compact_context 工具。 */
func (c *Compact) OnStart(_ context.Context, state *types.LoopState) error {
	state.Tools.Register(compactTool{})
	return nil
}

/* OnToolStart 拦截模型自主压缩。 */
func (c *Compact) OnToolStart(ctx context.Context, state *types.LoopState, call *types.ToolCall) (ezhook.Action, error) {
	if call.Name != CompactTool {
		return ezhook.Proceed, nil
	}
	var args struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(call.Args, &args)
	if state.ForkID != "" {
		if err := c.compactFork(ctx, state, false, args.Reason); err != nil {
			return ezhook.Skip("compact failed: " + err.Error()), nil
		}
		return ezhook.Skip("fork 上下文已压缩为摘要，请继续完成分身任务，不要重述已归档的过程。"), nil
	}
	info, err := c.compact(ctx, state, false, args.Reason)
	if err != nil {
		// 压缩失败不终止主循环：作为工具结果回传，模型继续原上下文作答。
		return ezhook.Skip("compact failed: " + err.Error()), nil
	}
	return ezhook.Skip("上下文已压缩归档（话题：" + info.Title + "），新会话已开启，系统提示已注入" +
		"摘要与上一会话路径。请直接继续回应用户，不要重述已归档的过程细节。"), nil
}

/*
OnEnd 收尾与水位自动压缩。挂起的压缩（工具路径）先收尾：旧库补写
完整历史并封存、切新库、新库跳过落盘。水位自动路径在同一处就地
压缩并收尾（轮末触发，无剩余迭代）。失败发 error 事件。
*/
func (c *Compact) OnEnd(ctx context.Context, state *types.LoopState) error {
	if c.pending != nil {
		c.finishPending(ctx, state)
		return nil
	}
	if c.threshold <= 0 || state.LastResponse == nil {
		return nil
	}
	if state.LastResponse.Usage.PromptTokens <= c.threshold {
		return nil
	}
	var err error
	if state.ForkID != "" {
		err = c.compactFork(ctx, state, true, "fork 上下文水位达到阈值")
	} else {
		_, err = c.compact(ctx, state, true, "上下文水位达到阈值")
	}
	if err != nil {
		state.EmitEvent(event.EventError, "auto compact failed: "+err.Error())
	}
	return nil
}

/*
finishPending 收尾一次挂起的压缩：过程消息归旧库 → 封存归档 →
切新 trace/新库（新库本轮不落盘）→ 内存翻页（只剩新 system，
下一轮用户输入即新库第一条）→ 发事件与回调。
*/
func (c *Compact) finishPending(ctx context.Context, state *types.LoopState) {
	p := c.pending
	c.pending = nil

	// 过程新增 = 截断列表 [system, handover, tail] 之后的追加
	// （工具结果、模型对压缩的汇总——逻辑上属于旧库的收尾）。
	var tailNew []types.Message
	if len(state.Messages) > 3 {
		tailNew = append(tailNew, state.Messages[3:]...)
	}
	full := append(append([]types.Message(nil), p.oldMsgs...), tailNew...)

	// 关闭压缩后重开的 turn root，残余 span 落旧 trace 后再切换。
	c.trace.CloseRoot(state, map[string]any{"stopReason": string(state.StopReason), "compacted": true})
	if err := c.archiveOld(ctx, p, state, full); err != nil {
		state.EmitEvent(event.EventError, "compact archive failed: "+err.Error())
	}
	_ = c.topics.Add(TopicEntry{
		ID:        p.oldID,
		Title:     p.title,
		Summary:   p.summary,
		CreatedAt: time.Now().UnixMilli(),
		Msgs:      len(full),
		Path:      SessionsDir + "/" + p.oldID,
		Kind:      "compact",
	})

	newID := p.newID
	c.trace.SetTrace(newID)
	c.sess.SetID(newID)
	c.sess.SetPrev(p.oldID, p.summary)
	c.sess.SkipNextSave() // 压缩轮不写新库：新库空置，等用户下一轮

	if len(state.Messages) > 0 && state.Messages[0].Role == types.RoleSystem {
		state.Messages = []types.Message{state.Messages[0]}
	} else {
		state.Messages = []types.Message{{Role: types.RoleSystem, Content: c.sys.Prompt()}}
	}

	info := CompactInfo{OldID: p.oldID, NewID: newID, Title: p.title, Summary: p.summary,
		Auto: p.auto, Reason: p.reason, PrevPath: SessionsDir + "/" + p.oldID}
	state.EmitEvent(EventCompact, info)
	if c.onCompact != nil {
		c.onCompact(info)
	}
}

/*
archiveOld 把压缩轮的完整历史补写进旧库存档并封存。system 用压缩前
的三段（摘要后的新 system 属于新库）；createdAt/input 等承自旧库上次
落盘的快照（压缩轮触发时旧库文件尚是上一轮的完整状态）。
*/
func (c *Compact) archiveOld(ctx context.Context, p *pendingCompact, state *types.LoopState, full []types.Message) error {
	snap := SessionSnap{
		ID:           p.oldID,
		CreatedAt:    state.StartedAt.UnixMilli(),
		Input:        state.Input,
		Messages:     stripSystem(full),
		SystemPrompt: p.oldPrompt,
		SystemBase:   p.oldBase,
		SummaryBlock: p.oldSummary,
		Tools:        toolNames(state),
		Iterations:   state.Iteration,
		StopReason:   string(state.StopReason),
		StartedAt:    state.StartedAt,
		EndedAt:      state.EndedAt,
		LastOutputAt: c.sess.LastOutputAt(),
		Archived:     true,
	}
	if old, err := LoadSnap(ctx, c.fsys, p.oldID); err == nil {
		snap.CreatedAt = old.CreatedAt
		snap.Input = old.Input
		snap.Model = old.Model
		if old.PrevSession != "" {
			snap.PrevSession, snap.CompactSummary = old.PrevSession, old.CompactSummary
		}
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return c.fsys.Write(ctx, SessionsDir+"/"+p.oldID+"/session.json", data)
}

/*
compact 执行主循环压缩：摘要 → 重组 system → 截断（供本轮剩余迭代
使用新上下文）→ 挂起 pending。切库/归档/事件延迟到 OnEnd 收尾——
压缩轮的完整历史（含工具结果与模型汇总）归旧库，新库空置起步。
auto 路径在 OnEnd 内就地挂起并立即收尾。
*/
func (c *Compact) compact(ctx context.Context, state *types.LoopState, auto bool, reason string) (CompactInfo, error) {
	if len(state.Messages) == 0 {
		return CompactInfo{}, errors.New("nothing to compact")
	}
	if c.pending != nil {
		return CompactInfo{}, errors.New("already compacted this turn")
	}
	oldID := c.sess.ID()
	title := firstUserTitle(state.Messages) // 截断前取：首条 user 在旧上下文里
	sp := c.trace.StartSpan(state, "compact", "compact", map[string]any{
		"auto": auto, "reason": reason, "oldId": oldID,
	})
	summaryText, err := c.summarize(ctx, state.Messages)
	if err != nil {
		c.trace.EndSpan(sp, map[string]any{"err": err.Error()})
		return CompactInfo{}, err
	}
	c.trace.EndSpan(sp, map[string]any{
		"title": title, "summary": truncStr(summaryText, 2048),
	})
	// compact span 记入旧 trace；压缩后的剩余迭代 span 也归旧 trace
	// （收尾时才 SetTrace），turn root 关闭后重开承接。
	c.trace.CloseRoot(state, map[string]any{"stopReason": "compacted", "compacted": true})
	if !auto {
		c.trace.OpenRoot(state, map[string]any{"input": "(compact 后继续)"})
	}

	prevPath := SessionsDir + "/" + oldID
	// 新 session 的 system：base 重组（compact 即创建新 session，记忆/skill/mcp 全量重载）
	// + 摘要段（含上一 session 路径引用）。
	summaryBlock := "# 上下文压缩存档\n上一会话已归档，原始记录在 " + prevPath +
		"（session.json 可读取全文）。本会话开始前的摘要：\n" + summaryText
	base := ""
	if c.rebuildBase != nil {
		base = c.rebuildBase()
	} else if b, _ := c.sys.Parts(); b != "" {
		base = b
	}
	oldBase, oldSummary := c.sys.Parts()
	oldPrompt := c.sys.Prompt()
	c.sys.Set(base, summaryBlock)

	// 截断：[新 system, handover, 衔接条]——末条保 tool_calls 协议配对
	// （工具路径末条是携带 tool_calls 的 assistant，引擎随后的结果追加紧贴它）；
	// 轮末路径丢弃孤儿 tool 尾巴（其配对的 assistant 已被截掉）。
	handover := types.Message{
		Role: types.RoleAssistant,
		Content: "（上下文已压缩）上一会话的摘要已注入系统提示，过程细节已归档，" +
			"如需细节可读取 " + prevPath + "/session.json。",
	}
	newMsgs := []types.Message{
		{Role: types.RoleSystem, Content: c.sys.Prompt()},
		handover,
	}
	newMsgs = append(newMsgs, keepTail(state.Messages, auto)...)
	oldMsgs := append([]types.Message(nil), state.Messages...)
	state.Messages = newMsgs

	newID := NewSessionID()
	c.pending = &pendingCompact{
		oldID: oldID, newID: newID, title: title, reason: reason,
		summary: summaryText, auto: auto, oldMsgs: oldMsgs,
		oldPrompt: oldPrompt, oldBase: oldBase, oldSummary: oldSummary,
	}
	if auto {
		c.finishPending(ctx, state)
	}

	info := CompactInfo{OldID: oldID, NewID: newID, Title: title, Summary: summaryText,
		Auto: auto, Reason: reason, PrevPath: prevPath}
	return info, nil
}

/*
compactFork 就地压缩 fork 增量：只摘 SeedLen 之后的消息（seed 摘要无
意义，主库已有），摘要拼进 fork 内 system（fork 无独立 systemPrompt，
不动主会话的 SysPrompt）；截断后 SeedLen 重置为 1，sessionstore 剥离
逻辑按新起点存增量。
*/
func (c *Compact) compactFork(ctx context.Context, state *types.LoopState, auto bool, reason string) error {
	if state.SeedLen <= 0 || state.SeedLen >= len(state.Messages) {
		return errors.New("fork has no incremental context")
	}
	inc := state.Messages[state.SeedLen:]
	summaryText, err := c.summarize(ctx, inc)
	if err != nil {
		return err
	}
	systemMsg := state.Messages[0] // seed 的 system（Fork 保留）
	systemMsg.Content += "\n\n# fork 上下文压缩\n（本分身此前的过程已压缩）摘要：\n" + summaryText
	handover := types.Message{
		Role:    types.RoleAssistant,
		Content: "（fork 上下文已压缩）细节已归档，请继续完成分身任务。",
	}
	newMsgs := []types.Message{systemMsg, handover}
	newMsgs = append(newMsgs, keepTail(state.Messages, auto)...)
	state.Messages = newMsgs
	state.SeedLen = 1
	return nil
}

/*
summarize 压缩消息历史。不走 summary.Summarize（内置 30s 超时是给
EndHook 防挂死设计的），压缩路径用独立 2 分钟预算——大上下文压缩
本就慢，被 30s 卡死会让每次压缩都失败。
*/
func (c *Compact) summarize(ctx context.Context, msgs []types.Message) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s", m.Role, m.Content)
		if m.Err != "" {
			fmt.Fprintf(&b, " (error: %s)", m.Err)
		}
		b.WriteByte('\n')
	}
	resp, err := c.provider.Invoke(ctx, &types.ModelRequest{Messages: []types.Message{
		{Role: types.RoleUser, Content: compactSummaryPrompt + "\n\n" + b.String()},
	}})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

/* firstUserTitle 从消息里取首条 user 文本作话题标题。 */
func firstUserTitle(msgs []types.Message) string {
	for _, m := range msgs {
		if m.Role == types.RoleUser {
			t := strings.TrimSpace(m.Content)
			if len([]rune(t)) > 40 {
				return string([]rune(t)[:40]) + "…"
			}
			return t
		}
	}
	return "未命名话题"
}

/*
keepTail 选截断后的衔接条：工具路径末条是携带 tool_calls 的 assistant
（引擎随后的结果追加必须紧贴它），保留；轮末路径末条若是 tool 消息
（配对的 assistant 已被截掉，会成为孤儿），向前回退到最近的非 tool 条。
*/
func keepTail(msgs []types.Message, auto bool) []types.Message {
	if len(msgs) == 0 {
		return nil
	}
	if !auto || msgs[len(msgs)-1].Role != types.RoleTool {
		return []types.Message{msgs[len(msgs)-1]}
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != types.RoleTool {
			return []types.Message{msgs[i]}
		}
	}
	return nil
}

/* compactTool 是 compact_context 壳工具（拦截式，Invoke 不可达）。 */
type compactTool struct{}

func (compactTool) Name() string { return CompactTool }
func (compactTool) Description() string {
	return "压缩上下文并开启全新会话：旧对话压缩为摘要注入系统提示并归档，上下文重置。" +
		"当用户明确想换话题、上下文过长影响专注、或状态栏提示推荐压缩时调用。"
}
func (compactTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"reason":{"type":"string","description":"压缩原因，一句话"}},"required":["reason"]}`)
}
func (compactTool) Invoke(context.Context, json.RawMessage) (string, error) {
	return "", errors.New("compact: hook not registered")
}
