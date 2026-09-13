/*
remind 是系统提醒 hook：统一负责"系统→模型"的全部单向旁路消息，
内部分三段（逻辑自闭，各段独立成模块防大杂烩）：

  - 快照段（每轮 OnStart）：<agent_status> 水位/时间快照 + status.snapshot
    SSE 事件（前端右上角水位条）——system 每 session 固定，水位与时间
    是轮内唯一需要同步的运行时状态；
  - 变更段（每轮 OnStart，reschange.go）：skill/mcp 基线 diff，
    有变化才插一条 <res_change> 消息 + res.change
    事件（按需、零噪音）；基线持久化随 session（重启不重复报）；
  - 收尾段（每轮 OnEnd）：<end_reason> 轮次/时长/结束原因 + 记录
    LastOutputAt（下轮"距上次输出"用）。须排在 sessionstore 落盘前
    ——快照与内存同源。

变更检测留在此处（而非 skilltool/mcp hook）的分界判据：检测依赖
session 域状态（Store 里的资源基线），而 skilltool/mcp 是可下沉
ezloop 的纯工具组件，不得耦合宿主 session 概念（见 docs/hooks.md）。
*/
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/types"
)

/* StatusTag 是水位快照记录的包裹标签。 */
const StatusTag = "agent_status"

/* EventStatus 是水位快照事件（Data 为 StatusData）。 */
const EventStatus = event.EventType("status.snapshot")

/* ResChangeTag 是资源变更记录的包裹标签（前端按它识别渲染变更卡）。 */
const ResChangeTag = "res_change"

/* EventResChange 是资源变更事件（Data 为 []string 变更条目）。 */
const EventResChange = event.EventType("res.change")

/* EndReasonTag 是轮终止记录的包裹标签。 */
const EndReasonTag = "end_reason"

/*
StatusData 是水位快照载荷（SSE status.snapshot 的 Data；注入消息
历史的正文是中文语义化文本，见 renderStatus）。
*/
type StatusData struct {
	Now                string `json:"now"`
	SinceLastOutputMin int64  `json:"sinceLastOutputMin"` // 0 = 无记录
	CtxTokens          int    `json:"ctxTokens"`
	CtxWindow          int    `json:"ctxWindow"`
	SuggestCompact     bool   `json:"suggestCompact"`
}

/* Remind 实现系统提醒（快照段 + 收尾段；变更段见 reschange.go）。 */
type Remind struct {
	fsys      fs.FileSystem
	store     *Store
	ctxTokens func() int
	ctxWindow int
	mcpList   func() []StatusMcp
	disabled  func() []string // 可空：禁用技能目录名（实时读设置快照）
}

/*
NewRemind 创建系统提醒 hook。ctxTokens 返回最近一次模型调用的 prompt
tokens；ctxWindow 是主模型上下文窗口（<=0 由调用方兜底默认）；
mcpList 返回启用的 server 清单（仅作变更基线）；disabled 可空。
*/
func NewRemind(fsys fs.FileSystem, store *Store, ctxTokens func() int, ctxWindow int,
	mcpList func() []StatusMcp, disabled func() []string) *Remind {
	return &Remind{fsys: fsys, store: store, ctxTokens: ctxTokens, ctxWindow: ctxWindow,
		mcpList: mcpList, disabled: disabled}
}

func (h *Remind) Name() string { return "remind" }

/* OnStart 轮首两段：变更段（有变化才说话）在前、快照段照旧每轮一条。 */
func (h *Remind) OnStart(ctx context.Context, state *types.LoopState) error {
	if items := h.buildChanges(ctx); len(items) > 0 {
		h.insertBeforeLastUser(state, types.Message{Role: types.RoleUser, Content: renderResChange(items)})
		state.EmitEvent(EventResChange, items)
	}
	data := h.buildSnapshot()
	h.insertBeforeLastUser(state, types.Message{Role: types.RoleUser,
		Content: "<" + StatusTag + ">\n" + renderStatus(data) + "\n</" + StatusTag + ">"})
	state.EmitEvent(EventStatus, data)
	return nil
}

/*
	insertBeforeLastUser 插入到末条 user 消息之前（startHooks 运行时

末条必为本轮 input）；无 user 时追加。
*/
func (h *Remind) insertBeforeLastUser(state *types.LoopState, msg types.Message) {
	if n := len(state.Messages); n > 0 && state.Messages[n-1].Role == types.RoleUser {
		state.Messages = slices.Insert(state.Messages, n-1, msg)
	} else {
		state.Messages = append(state.Messages, msg)
	}
}

/*
buildSnapshot 组装水位快照数据（时间/距上次输出/水位/压缩建议）。
*/
func (h *Remind) buildSnapshot() StatusData {
	now := time.Now()
	data := StatusData{
		Now:       now.Format("2006-01-02 15:04"),
		CtxTokens: h.ctxTokens(),
		CtxWindow: h.ctxWindow,
	}
	if last := h.store.LastOutputAt(); last > 0 {
		data.SinceLastOutputMin = (now.UnixMilli() - last) / 60000
	}
	if h.ctxWindow > 0 && data.CtxTokens > h.ctxWindow*7/10 {
		data.SuggestCompact = true
	}
	return data
}

/*
renderStatus 把快照数据渲染成模型可读的中文文本（行格式是前后端契约：
前端历史重建按关键词"整理上下文"识别异常行进时间线，普通轮次只在
右上角——改行格式须同步前端解析）。
*/
func renderStatus(d StatusData) string {
	var b strings.Builder
	b.WriteString("当前时间：" + d.Now)
	if d.CtxWindow > 0 {
		fmt.Fprintf(&b, "\n上下文水位：%d / %d tokens", d.CtxTokens, d.CtxWindow)
		if d.SuggestCompact {
			b.WriteString("（已超窗口 70%，建议调用 trim_context 整理上下文）")
		}
	}
	if d.SinceLastOutputMin > 0 {
		fmt.Fprintf(&b, "\n距上次输出：%d 分钟", d.SinceLastOutputMin)
	}
	return b.String()
}

/* renderResChange 渲染资源变更记录（条目行前缀 "- " 是前端解析契约）。 */
func renderResChange(items []string) string {
	var b strings.Builder
	b.WriteString("<" + ResChangeTag + ">")
	b.WriteString("\n（系统检测到的本轮资源变更，非用户发言，无需回应，无需回溯处理）")
	for _, it := range items {
		b.WriteString("\n- " + it)
	}
	b.WriteString("\n</" + ResChangeTag + ">")
	return b.String()
}

/*
OnEnd 收尾段：先记录最近输出时间（下轮"距上次输出"用，无条件——
含 fork），fork 历史只存增量不记收尾（终止语义归主循环），随后追加
<end_reason>（轮次/时长/结束时间/原因；本轮发生过话题压缩时附说明
——压缩翻页后本记录落在新会话开头，需自解释，否则模型莫名收到一条
来历不明的消息）。
*/
func (h *Remind) OnEnd(_ context.Context, state *types.LoopState) error {
	h.store.SetLastOutputAt(time.Now().UnixMilli())
	if state.ForkID != "" {
		return nil
	}
	var b strings.Builder
	b.WriteString("<" + EndReasonTag + ">")
	b.WriteString("\n（系统自动记录的轮次收尾信息，非用户发言，无需回应）")
	fmt.Fprintf(&b, "\n运行轮次：%d", state.Iteration)
	fmt.Fprintf(&b, "\n运行时长：%s", humanDur(time.Since(state.StartedAt))) // EndedAt 在 endHooks 全部跑完后才设置
	fmt.Fprintf(&b, "\n结束时间：%s", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "\n结束原因：%s", FriendlyStop(string(state.StopReason)))
	if state.LastError != nil {
		fmt.Fprintf(&b, "\n错误详情：%s", oneLine(state.LastError.Error(), 300))
	}
	if state.Metadata["compacted"] == true {
		b.WriteString("\n话题压缩：本轮结束时上下文水位达到阈值，上一会话已压缩归档并开启新会话，" +
			"本条记录随翻页落在新会话开头，仅用于说明上一会话的收尾情况。")
	}
	b.WriteString("\n</" + EndReasonTag + ">")
	state.AppendMessage(types.Message{Role: types.RoleUser, Content: b.String()})
	return nil
}

/*
	oneLine 压平换行并按 rune 截断（错误详情进 end_reason 单行展示，

HTTP 错误响应体可能多行或超长）。
*/
func oneLine(s string, max int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

/* humanDur 把轮耗时渲染成中文短语（"45 秒"、"3 分 12 秒"）。 */
func humanDur(d time.Duration) string {
	switch {
	case d <= 0:
		return "不足 1 秒"
	case d < time.Minute:
		return fmt.Sprintf("%.0f 秒", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%d 分 %d 秒", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%d 小时 %d 分钟", int(d.Hours()), int(d.Minutes())%60)
	}
}

/* FriendlyStop 把轮停止原因映射为终止分类（end_reason 记录用）。 */
func FriendlyStop(reason string) string {
	switch reason {
	case "completed":
		return "正常结束"
	case "cancelled":
		return "手动停止"
	case "max_iterations":
		return "达到迭代上限"
	case "aborted":
		return "策略中止"
	case "error":
		return "执行出错"
	case "":
		return "正常结束"
	}
	return "本轮结束（" + reason + "）"
}

/* ParseStatusTag 从消息内容解析 <agent_status> 载荷（非状态记录返回 nil）。 */
func ParseStatusTag(content string) *StatusData {
	open, close := "<"+StatusTag+">", "</"+StatusTag+">"
	i := strings.Index(content, open)
	if i < 0 {
		return nil
	}
	rest := content[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return nil
	}
	var d StatusData
	if json.Unmarshal([]byte(strings.TrimSpace(rest[:j])), &d) != nil {
		return nil
	}
	return &d
}

/*
	LastCtxTokens 从历史尾部找最近一条状态记录的水位（旧快照无

ctxTokens 字段时的恢复兜底；找不到返回 0）。
*/
func LastCtxTokens(msgs []types.Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != types.RoleUser {
			continue
		}
		if d := ParseStatusTag(msgs[i].Content); d != nil && d.CtxTokens > 0 {
			return d.CtxTokens
		}
	}
	return 0
}
