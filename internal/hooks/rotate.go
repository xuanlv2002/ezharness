/*
rotate 是话题轮换 hook：模型自主调用 rotate_context 工具、或轮末水位
超阈值时自动触发——压缩旧话题入档、以交接摘要开启新 session。
对用户 session 完全透明，是 agent 的"记忆翻页"机制。

实现要点：轮内直接截断 state.Messages 并 SetID 新会话——引擎随后的
tool 结果追加与 OnEnd 落盘天然作用于新上下文，无需引擎任何配合。
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
	"github.com/xuanlv2002/ezloop/ext/hook/localsession"
	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
)

/* RotateTool 是话题轮换工具的注册名。 */
const RotateTool = "rotate_context"

/* EventRotate 是话题轮换事件（主循环专属，Data 为 RotateInfo）。 */
const EventRotate = event.EventType("rotate.topic")

/* RotateInfo 描述一次话题轮换。 */
type RotateInfo struct {
	OldID  string `json:"oldId"`
	NewID  string `json:"newId"`
	Title  string `json:"title"`
	Auto   bool   `json:"auto"`
	Reason string `json:"reason,omitempty"`
}

const rotateSummaryPrompt = "为切换到全新话题生成交接摘要：保留上一话题的关键事实、" +
	"已达成的决定、未完成的待办与用户偏好；忽略过程细节；简洁自包含，300 字以内。"

/* Rotate 提供模型自主与水位自动两条轮换路径。 */
type Rotate struct {
	provider  provider.ModelProvider
	fsys      fs.FileSystem
	sess      *localsession.Hook
	topics    *Topics
	threshold int // 水位阈值（prompt tokens），<=0 禁用自动轮换
	onRotate  func(RotateInfo) // 轮换成功回调（宿主同步自己的 session 标识）
}

/* NewRotate 创建轮换 hook。onRotate 可为 nil。 */
func NewRotate(p provider.ModelProvider, fsys fs.FileSystem, sess *localsession.Hook,
	topics *Topics, threshold int, onRotate func(RotateInfo)) *Rotate {
	return &Rotate{provider: p, fsys: fsys, sess: sess, topics: topics, threshold: threshold, onRotate: onRotate}
}

func (r *Rotate) Name() string { return "rotate" }

/* OnStart 注册 rotate_context 工具。 */
func (r *Rotate) OnStart(_ context.Context, state *types.LoopState) error {
	state.Tools.Register(rotateTool{})
	return nil
}

/* OnToolStart 拦截模型自主轮换。 */
func (r *Rotate) OnToolStart(ctx context.Context, state *types.LoopState, call *types.ToolCall) (ezhook.Action, error) {
	if call.Name != RotateTool {
		return ezhook.Proceed, nil
	}
	if state.ForkID != "" {
		return ezhook.Skip("rotate: not allowed in fork"), nil
	}
	var args struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(call.Args, &args)
	info, err := r.rotate(ctx, state, false, args.Reason)
	if err != nil {
		// 轮换失败不终止主循环：作为工具结果回传，模型继续原话题作答。
		return ezhook.Skip("rotate failed: " + err.Error()), nil
	}
	return ezhook.Skip("话题已归档（" + info.Title + "），新会话已开启，上下文已重置为交接摘要。" +
		"请直接回应用户的新话题，不要再引用已归档的过程细节。"), nil
}

/*
OnEnd 水位自动轮换：最近一次模型调用的 prompt tokens 超阈值即翻页。
失败静默（轮已结束，错误无处安放），只保证不破坏本轮历史。
*/
func (r *Rotate) OnEnd(ctx context.Context, state *types.LoopState) error {
	if r.threshold <= 0 || state.ForkID != "" || state.LastResponse == nil {
		return nil
	}
	if state.LastResponse.Usage.PromptTokens <= r.threshold {
		return nil
	}
	if _, err := r.rotate(ctx, state, true, "上下文水位达到阈值"); err != nil {
		state.EmitEvent(event.EventError, "auto rotate failed: "+err.Error())
	}
	return nil
}

/* rotate 执行轮换：摘要 → 入档 → 截断上下文 → 切新 session → 发事件。 */
func (r *Rotate) rotate(ctx context.Context, state *types.LoopState, auto bool, reason string) (RotateInfo, error) {
	if len(state.Messages) == 0 {
		return RotateInfo{}, errors.New("nothing to rotate")
	}
	oldID := r.sess.ID()
	title := firstUserTitle(state.Messages) // 截断前取：首条 user 在旧上下文里
	summaryText, err := r.summarize(ctx, state.Messages)
	if err != nil {
		return RotateInfo{}, err
	}

	// 截断：交接摘要打头，原末条消息保留在尾部——
	// 工具路径末条是携带 tool_calls 的 assistant（结果配对协议），
	// 轮末路径末条是本轮最后的消息（下一轮 WithHistory 的衔接点）。
	handover := types.Message{
		Role: types.RoleAssistant,
		Content: "（话题交接存档）上一话题的摘要如下，过程细节已归档，" +
			"如需细节可用 recall_topic 回顾：\n" + summaryText,
	}
	msgCount := len(state.Messages)
	state.Messages = append([]types.Message{handover}, state.Messages[len(state.Messages)-1])

	newID := localsession.NewID()
	r.sess.SetID(newID)
	_ = r.topics.Add(TopicEntry{
		ID:        oldID,
		Title:     title,
		Summary:   summaryText,
		CreatedAt: time.Now().UnixMilli(),
		Msgs:      msgCount,
	})

	info := RotateInfo{OldID: oldID, NewID: newID, Title: title, Auto: auto, Reason: reason}
	state.EmitEvent(EventRotate, info)
	if r.onRotate != nil {
		r.onRotate(info)
	}
	return info, nil
}

/*
summarize 压缩消息历史。不走 summary.Summarize（内置 30s 超时是给
EndHook 防挂死设计的），轮换路径用独立 2 分钟预算——大上下文压缩
本就慢，被 30s 卡死会让每次轮换都失败。
*/
func (r *Rotate) summarize(ctx context.Context, msgs []types.Message) (string, error) {
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
	resp, err := r.provider.Invoke(ctx, &types.ModelRequest{Messages: []types.Message{
		{Role: types.RoleUser, Content: rotateSummaryPrompt + "\n\n" + b.String()},
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

/* rotateTool 是 rotate_context 壳工具（拦截式，Invoke 不可达）。 */
type rotateTool struct{}

func (rotateTool) Name() string        { return RotateTool }
func (rotateTool) Description() string {
	return "归档当前话题并开启全新会话：旧对话压缩为摘要存档、上下文重置。" +
		"当用户明确想换话题、或感到上下文过长影响专注时调用。"
}
func (rotateTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"reason":{"type":"string","description":"轮换原因，一句话"}},"required":["reason"]}`)
}
func (rotateTool) Invoke(context.Context, json.RawMessage) (string, error) {
	return "", errors.New("rotate: hook not registered")
}
