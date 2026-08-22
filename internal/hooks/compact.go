/*
compact 是上下文压缩 hook：模型主动调 compact_context 工具、或轮末
水位超阈值（prompt tokens > threshold）时自动触发。主循环路径：
总结当前 session → 重组 system（base 重读记忆/skill/mcp + 摘要段 +
上一 session 路径引用）→ 截断上下文 → 切换全新 session → 旧库封存
归档到话题记忆 → 发 session.compact 事件（前端渲染分隔线并更新
activeId）。fork 路径：就地压缩增量（不换库不归档，摘要拼进 fork
内的 system）。

对用户 session 完全透明，是 agent 的"记忆翻页"机制。实现沿 rotate
的轮内截断方案，修正其丢失 system 的缺陷（截断保留重写后的 system）。
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

/* OnEnd 水位自动压缩：最近一次模型调用的 prompt tokens 超阈值即翻页。失败发 error 事件。 */
func (c *Compact) OnEnd(ctx context.Context, state *types.LoopState) error {
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
		var info CompactInfo
		info, err = c.compact(ctx, state, true, "上下文水位达到阈值")
		if err == nil && c.onCompact != nil {
			c.onCompact(info)
		}
	}
	if err != nil {
		state.EmitEvent(event.EventError, "auto compact failed: "+err.Error())
	}
	return nil
}

/* compact 执行主循环压缩：摘要 → 重组 system → 截断 → 切新 session → 归档 → 事件。 */
func (c *Compact) compact(ctx context.Context, state *types.LoopState, auto bool, reason string) (CompactInfo, error) {
	if len(state.Messages) == 0 {
		return CompactInfo{}, errors.New("nothing to compact")
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
	// compact span 记入旧 trace；此后关闭旧 turn、切换新 trace。
	c.trace.CloseRoot(state, map[string]any{"stopReason": "compacted", "compacted": true})

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
	msgCount := len(state.Messages)
	state.Messages = newMsgs

	newID := NewSessionID()
	c.sess.SetID(newID)
	c.sess.SetPrev(oldID, summaryText)
	c.trace.SetTrace(newID)
	if auto {
		// 轮末路径 turn 已关闭；工具路径本轮还有剩余迭代，重开 turn 承接。
		c.trace.OpenRoot(state, map[string]any{"input": "(compact 后继续)"})
	}

	_ = MarkArchived(ctx, c.fsys, oldID)
	_ = c.topics.Add(TopicEntry{
		ID:        oldID,
		Title:     title,
		Summary:   summaryText,
		CreatedAt: time.Now().UnixMilli(),
		Msgs:      msgCount,
		Path:      prevPath,
		Kind:      "compact",
	})

	info := CompactInfo{OldID: oldID, NewID: newID, Title: title, Summary: summaryText,
		Auto: auto, Reason: reason, PrevPath: prevPath}
	state.EmitEvent(EventCompact, info)
	if c.onCompact != nil {
		c.onCompact(info)
	}
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
