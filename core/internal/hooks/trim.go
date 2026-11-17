/*
trim 是上下文整理 hook：水位超阈值（OnLoop，每次迭代回边）或模型
主动调 trim_context 工具时就地折叠上下文——早期交互总结为摘要，以
<context_trim kept="N"> marker 消息衔接，立即生效（下一次模型调用即
新上下文）。会话身份不变：不换库、不切 trace、不动话题线。

消息序列纯追加：OnLoop 就地截断（立即生效），marker 暂存后在本轮
OnEnd 落位——追加在 <end_reason> 之后，一次完整交互的最下方，历史
不回插、不改写。保留按 user 输入定界：kept=N 保留最近 N+1 次输入的
完整交互（触发整理的这次恒在内，N 是它之上再保留的次数）；但至少
折叠最早的 1 个交互，交互不足时少留直至整段折叠（kept=-1）。下轮取
上下文时从最后一个 marker 向上回溯 kept+1 次完整交互作为视图起点
（ViewStart）——边界落在 user 输入处，不会切出孤儿 tool 序列。

并发契约：OnToolStart 在同轮多调用间并发（ezloop 契约），此路径只
登记 pending（锁保护）不做任何 state 写；截断统一延迟到 OnLoop——
引擎串行区，且此刻本轮全部工具结果（含 Skip 结果）已按序入史，消息
序列协议完整，无读写竞争。stashed 只在 OnLoop 与 OnEnd（同为引擎
串行区）读写，无需加锁；注册序须保证 remind（end_reason）在前、
sessionstore（落盘）在后，marker 才落得进本轮档案。

存储为追加式档案：折叠段挂 state.Metadata，sessionstore 落盘与
domain FinishRun 经 MergeFull 合成全量——渲染时间线可见整理位置。
fork 同样支持（折叠 SeedLen 之后的增量）。
*/
package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/xuanlv2002/ezloop/event"
	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
)

/* TrimTool 是模型主动整理上下文的注册工具名。 */
const TrimTool = "trim_context"

/* TrimTag 是整理 marker 的包裹标签（渲染/过滤据此识别）。 */
const TrimTag = "context_trim"

/* EventTrim 是上下文整理完成事件（Data 为 TrimInfo）。 */
const EventTrim = event.EventType("session.trim")

/* EventTrimming 是整理开始事件（摘要最多 2 分钟，静默会被当成卡死）。 */
const EventTrimming = event.EventType("session.trimming")

/* TrimInfo 描述一次上下文整理。 */
type TrimInfo struct {
	Tokens  int    `json:"tokens"`            // 触发时的水位（自动路径；工具路径为 0）
	Folded  int    `json:"folded"`            // 折叠的早期消息条数
	Kept    int    `json:"kept"`              // 保留的完整交互数（0 = 整段折叠）
	Fork    bool   `json:"fork"`              // 是否分身内的整理
	Summary string `json:"summary,omitempty"` // 结构化摘要全文（前端整理条点开看的内容）
}

const trimSummaryPrompt = "你在整理一段 agent 工作对话的早期上下文，为\"接下来的你\"留存衔接材料。" +
	"按固定结构输出，保留四个小节标题，无内容的小节写\"无\"：\n" +
	"【已完成】\n- 已完成的事项与关键结果（改了哪些文件、命令产出了什么、得到的结论）\n" +
	"【正在做】\n- 当前进行中的事项与进行到哪一步\n" +
	"【待办】\n- 已明确但尚未开始或未完成的事项\n" +
	"【关键事实】\n- 后续必须知道的信息：用户要求与偏好、已定的技术决策、路径/ID/终端/标签等关键标识\n" +
	"要求：每条一行、信息密度优先；忽略工具原始输出的过程细节与状态栏记录；" +
	"输入中若出现此前整理产生的旧摘要，把其内容并入对应小节继续浓缩；" +
	"总长 400 字以内；这是自我交接材料，不是给人看的总结。"

/*
	trimKeepUser 是 marker kept 属性默认值：触发整理的这次 user 输入之外，

再向上保留的完整交互条数（1 = 共保留最近两次输入，0 = 只留触发这次）。
*/
const trimKeepUser = 1

/* trimFoldedKey 是 state.Metadata 里折叠段的键（sessionstore 与 domain 共读）。 */
const trimFoldedKey = "trim_folded"

/* trimDone 是一次已执行整理的落位物（OnLoop 产出，OnEnd 消费）。 */
type trimDone struct {
	marker types.Message
	info   TrimInfo
}

/* Trim 提供水位自动与模型主动两条整理路径（统一在 OnLoop 串行区执行）。 */
type Trim struct {
	provider    provider.ModelProvider
	trace       *Trace
	threshold   int           // 水位阈值（prompt tokens），<=0 禁用自动整理
	window      int           // 模型窗口（提示展示水位比例用）
	archivePath func() string // 可空：会话存档绝对路径（marker 里给原文地址）

	mu      sync.Mutex                    // 并发契约：pending 登记互斥
	pending map[*types.LoopState]bool     // 排队的整理（OnToolStart 登记，OnLoop 消费）
	stashed map[*types.LoopState]trimDone // 已执行的整理（OnLoop 写，OnEnd 落位消费）
}

/*
NewTrim 创建整理 hook。archivePath 可空（裸装配与测试）：marker 回退为

	「路径见 <session> 块」。
*/
func NewTrim(p provider.ModelProvider, trace *Trace, threshold, window int, archivePath func() string) *Trim {
	return &Trim{provider: p, trace: trace, threshold: threshold, window: window,
		archivePath: archivePath, pending: map[*types.LoopState]bool{}, stashed: map[*types.LoopState]trimDone{}}
}

func (t *Trim) Name() string { return "trim" }

/* OnStart 注册 trim_context 工具（使用说明走 Guide 声明）。 */
func (t *Trim) OnStart(_ context.Context, state *types.LoopState) error {
	state.Tools.Register(trimTool{})
	return nil
}

/* Guide 声明 trim_context 的使用说明（引擎组装期统一追加，顺序与幂等由引擎保证）。 */
func (t *Trim) Guide() string {
	return "<tool-guide>\ntrim_context：把早期对话就地折叠为结构化摘要" +
		"（已完成/正在做/待办/关键事实），上下文立即变小，会话与历史档案不变。" +
		"感觉上下文过长影响专注或质量时主动调用，无需用户同意。" +
		"整理保留最近两次用户输入的完整交互，调用后直接继续当前任务，不要重述将被折叠的过程细节；" +
		"整理后需要更大范围的回忆时读会话存档（路径见 <session> 块）。\n</tool-guide>"
}

/*
OnLoop 统一执行整理（引擎串行区）。两个来源：工具排队（OnToolStart
登记——本批工具结果已全部入史，含 trim_context 的 Skip 结果）与水位
自动（PromptTokens 超阈值）。Metadata 防同轮重复（整理后 LastResponse
用量仍是旧值；下一轮 state 新建自动复位）。
*/
func (t *Trim) OnLoop(ctx context.Context, state *types.LoopState) error {
	t.mu.Lock()
	queued := t.pending[state]
	delete(t.pending, state) // 消费即清（防 state 泄漏）
	t.mu.Unlock()

	if state.Metadata["trimmed"] == true {
		return nil
	}
	tokens := 0
	if queued {
		tokens = state.LastResponse.Usage.PromptTokens
	} else {
		if t.threshold <= 0 || state.LastResponse == nil {
			return nil
		}
		tokens = state.LastResponse.Usage.PromptTokens
		if tokens <= t.threshold {
			return nil
		}
		msg := fmt.Sprintf("上下文水位 %d tokens，达到阈值，正在整理…", tokens)
		if t.window > 0 {
			msg = fmt.Sprintf("上下文水位 %d / %d tokens（%d%%），达到阈值，正在整理…",
				tokens, t.window, tokens*100/t.window)
		}
		state.EmitEvent(EventTrimming, msg)
	}
	if err := t.doTrim(ctx, state, tokens); err != nil {
		state.EmitEvent(event.EventError, "trim failed: "+err.Error())
	}
	return nil
}

/*
OnEnd 插入摘要 marker：追加在本轮 <end_reason> 之后（注册序在 remind
之后、sessionstore 之前）——一次完整交互的最下方，时间序自然；随后
发整理完成事件（前端卡片与重载后的位置一致）。
*/
func (t *Trim) OnEnd(_ context.Context, state *types.LoopState) error {
	done, ok := t.stashed[state]
	if !ok {
		return nil
	}
	delete(t.stashed, state)
	state.AppendMessage(done.marker)
	state.EmitEvent(EventTrim, done.info)
	return nil
}

/*
OnToolStart 拦截模型主动整理：只登记排队（锁保护，同轮重复调用提示），
不写任何 state——本路径在并发回调区，截断延到 OnLoop 串行区执行
（同一迭代回边，下一次模型调用前生效）。
*/
func (t *Trim) OnToolStart(_ context.Context, state *types.LoopState, call *types.ToolCall) (ezhook.Action, error) {
	if call.Name != TrimTool {
		return ezhook.Proceed, nil
	}
	t.mu.Lock()
	if t.pending[state] {
		t.mu.Unlock()
		return ezhook.Skip("本轮已安排整理，无需重复调用，请继续当前任务。"), nil
	}
	t.pending[state] = true
	t.mu.Unlock()
	return ezhook.Skip("已安排整理：本批工具调用完成后立即折叠早期上下文，" +
		"请继续当前任务，不要重述将被折叠的过程细节。"), nil
}

/*
doTrim 执行整理：OnLoop 就地截断——只保留当前段（本轮输入 + trim 调用）
起、向前的 keep+1 个完整交互，更早的全部折进档案，下一次模型调用即新
上下文；marker 暂存，OnEnd 时落到本轮 <end_reason> 之后。段数不足
keep+1 时全部保留，摘要卡照插（折 0 条）。失败不改变上下文（放弃本轮，
模型继续原上下文作答）。折叠段挂 state.Metadata（sessionstore 与
domain 共读）。
*/
func (t *Trim) doTrim(ctx context.Context, state *types.LoopState, tokens int) error {
	msgs := state.Messages
	if len(msgs) == 0 {
		return errors.New("nothing to trim")
	}
	fork := state.ForkID != ""
	start := 1 // 主循环跳过 system
	if fork {
		if state.SeedLen <= 0 || state.SeedLen >= len(msgs) {
			return errors.New("fork has no incremental context")
		}
		start = state.SeedLen // seed 归主库，只折叠 fork 增量
	}
	fold := msgs[start:]
	var inputs []int // 折叠区内真实 user 输入的位置
	for i, m := range fold {
		if isUserInput(m) {
			inputs = append(inputs, i)
		}
	}
	retained := trimKeepUser + 1
	if retained > len(inputs) {
		retained = len(inputs) // 段数不足：全部保留，仅插摘要卡
	}
	boundary := 0
	if retained < len(inputs) {
		boundary = inputs[len(inputs)-retained]
		for boundary > 0 && isOpeningInject(fold[boundary-1]) {
			boundary-- // 开场注入（status 等）随本轮输入一起保留
		}
	}
	sumInput := fold[:boundary]

	var sp *Span
	if t.trace != nil {
		sp = t.trace.StartSpan(state, "trim", "trim", map[string]any{
			"fork": fork, "tokens": tokens, "fold": boundary,
		})
	}
	summaryText, err := summarizeMsgs(ctx, t.provider, trimSummaryPrompt, sumInput)
	if err != nil {
		if t.trace != nil {
			t.trace.EndSpan(sp, map[string]any{"err": err.Error()})
		}
		return err
	}
	summaryText = normalizeStructuredSummary(summaryText)
	if t.trace != nil {
		t.trace.EndSpan(sp, map[string]any{"summary": truncStr(summaryText, 2048)})
	}

	archiveLoc := "原始记录仍完整保留在会话档案中（路径见 <session> 块）"
	if t.archivePath != nil {
		if p := t.archivePath(); p != "" {
			archiveLoc = "原始记录仍完整保留在 " + p + "（可读取全文）"
		}
	}
	kept := retained - 1
	if kept < 0 {
		kept = 0
	}
	marker := types.Message{Role: types.RoleUser, Content: "<" + TrimTag +
		` kept="` + strconv.Itoa(kept) + `">` +
		"\n（系统自动整理，非用户发言，无需回应）" +
		"\n此前的早期对话已折叠出模型上下文（" + archiveLoc + "）。\n当前任务衔接摘要：\n" +
		summaryText + "\n</" + TrimTag + ">"}

	if state.Metadata == nil {
		state.Metadata = map[string]any{}
	}
	state.Metadata["trimmed"] = true
	state.Metadata[trimFoldedKey] = append(FoldedOf(state), fold[:boundary]...)
	// 头部不动（主循环 system / fork 完整 seed），保留段承前启后；
	// marker 暂存待 OnEnd 落位
	next := make([]types.Message, 0, start+len(fold)-boundary)
	next = append(next, msgs[:start]...)
	next = append(next, fold[boundary:]...)
	state.Messages = next
	t.stashed[state] = trimDone{marker: marker, info: TrimInfo{
		Tokens: tokens, Folded: boundary, Kept: retained, Fork: fork, Summary: summaryText}}
	return nil
}

/* FoldedOf 取 state 上累积的折叠段（无则空）。 */
func FoldedOf(state *types.LoopState) []types.Message {
	if state == nil {
		return nil
	}
	if m, ok := state.Metadata[trimFoldedKey].([]types.Message); ok {
		return m
	}
	return nil
}

/*
ViewStart 返回模型视图在全量历史中的起点：从最后一个整理 marker 向上
回溯 kept+1 次完整交互（kept 属性记录整理当时的保留值），起点即整理
时定下的折叠边界。无 marker 或回溯不满时 0（全量）。modelView
（domain）与 MergeFull（落盘/FinishRun）共用，保证两处对"本轮视图
覆盖了 last 的哪一段"判断一致。
*/
func ViewStart(history []types.Message) int {
	for i := len(history) - 1; i >= 0; i-- {
		if !IsTrimMarker(history[i]) {
			continue
		}
		return groupStart(history, i, markerKept(history[i]))
	}
	return 0
}

/* markerKept 解析 marker 的 kept 属性（= 保留交互数 - 1；解析失败按 0）。 */
func markerKept(m types.Message) int {
	s := m.Content
	i := strings.Index(s, `kept="`)
	if i < 0 {
		return 0
	}
	rest := s[i+len(`kept="`):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil || n < 0 {
		return 0
	}
	return n
}

/*
MergeFull 合成全量历史：已知全量（上一轮末）截到本轮视图起点 + 本轮
折叠段 + 当前消息。视图覆盖段由 ViewStart 判定（与 modelView 同源），
其前的档案从未进过本轮视图，必须保留——否则跨轮覆盖丢档。落盘
（store.OnEnd，last=盘上快照）与内存（FinishRun，last=history）共用。
*/
func MergeFull(last []types.Message, state *types.LoopState) []types.Message {
	cut := ViewStart(last)
	folded, msgs := FoldedOf(state), []types.Message{}
	if state != nil {
		msgs = state.Messages
	}
	out := make([]types.Message, 0, cut+len(folded)+len(msgs))
	out = append(out, last[:cut]...)
	out = append(out, folded...)
	out = append(out, msgs...)
	return out
}

/*
groupStart 从 from（不含）向上数第 keep+1 条真实 user 输入，返回其
所在轮的起点（再向上吞并该轮开场注入——插在本轮输入之前的
<reference_file>/<resource_change>/<agent_status>，起点恰落在上一轮
<end_reason> 之后）。数不满返回 0。doTrim 以段尾为 from 定折叠边界；
ViewStart 以最后一个 marker 为 from 回溯视图起点。
*/
func groupStart(msgs []types.Message, from, keep int) int {
	count := 0
	for i := from - 1; i >= 0; i-- {
		if !isUserInput(msgs[i]) {
			continue
		}
		count++
		if count == keep+1 {
			for i > 0 && isOpeningInject(msgs[i-1]) {
				i--
			}
			return i
		}
	}
	return 0
}

/* isUserInput 报告消息是否为用户真实输入（<tag> 系统注入与引擎收尾提醒不算）。 */
func isUserInput(m types.Message) bool {
	return m.Role == types.RoleUser &&
		!strings.HasPrefix(m.Content, "<") &&
		!strings.HasPrefix(m.Content, "（系统提醒：")
}

/* isOpeningInject 报告消息是否为某轮开场的系统注入（插在该轮 user 输入之前）。 */
func isOpeningInject(m types.Message) bool {
	return m.Role == types.RoleUser &&
		(strings.HasPrefix(m.Content, "<"+StatusTag) ||
			strings.HasPrefix(m.Content, "<"+ResChangeTag) ||
			strings.HasPrefix(m.Content, "<"+RefTag))
}

/* trimTool 是 trim_context 壳工具（拦截式，Invoke 不可达）。 */
type trimTool struct{}

func (trimTool) Name() string { return TrimTool }
func (trimTool) Description() string {
	return "整理上下文：把早期对话就地折叠为摘要，上下文立即变小，会话与历史档案不变。" +
		"当感觉上下文过长影响专注、或状态栏提示水位过高时调用。"
}
func (trimTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}
func (trimTool) Invoke(context.Context, json.RawMessage) (string, error) {
	return "", errors.New("trim: hook not registered")
}

/* IsTrimMarker 报告消息是否为整理 marker（域层过滤/前端识别共用）。 */
func IsTrimMarker(m types.Message) bool {
	return strings.HasPrefix(m.Content, "<"+TrimTag)
}
