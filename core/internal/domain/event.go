/*
事件帧映射：ezloop 事件 → SSE JSON 帧，一处集中（原 dto.go 平移）。
*/
package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/hooks"
)

/* Event 是 SSE 帧（data: {...}）。 */
type Event struct {
	Type   string          `json:"type"`
	Ts     int64           `json:"ts"`
	Iter   int             `json:"iter"`
	ForkID string          `json:"forkId,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

/* ToolStartData 是 tool_start 的数据。 */
type ToolStartData struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

/* ToolEndData 是 tool_end 的数据。 */
type ToolEndData struct {
	CallID  string `json:"callId"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Err     string `json:"err,omitempty"`
}

/* ModelEndData 是 model_end 的数据（ModelResponse 摘要 + 水位）。 */
type ModelEndData struct {
	Content   string        `json:"content"`
	Reasoning string        `json:"reasoning,omitempty"`
	ToolCalls []ToolStartID `json:"toolCalls,omitempty"`
	Usage     *types.Usage  `json:"usage,omitempty"`
}

/* ToolStartID 是 model_end 携带的调用标识。 */
type ToolStartID struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

/* TaskStartData 是 task.start 的数据。 */
type TaskStartData struct {
	ID     string `json:"id"`
	Task   string `json:"task"`
	CallID string `json:"callId"`
}

/* TaskEndData 是 task.end 的数据。 */
type TaskEndData struct {
	StopReason string `json:"stopReason"`
	Iterations int    `json:"iterations"`
	Answer     string `json:"answer"`
}

/* TurnEndData 是合成的 turn_end 帧。 */
type TurnEndData struct {
	StopReason string       `json:"stopReason"`
	Err        string       `json:"err,omitempty"`
	Iterations int          `json:"iterations"`
	ElapsedMs  int64        `json:"elapsedMs,omitempty"` // 本轮总耗时（前端终止提示用）
	Usage      *types.Usage `json:"usage,omitempty"`
}

/* MapEvent 把 ezloop 事件归一为 SSE 帧。 */
func MapEvent(e event.Event) Event {
	out := Event{Type: string(e.Type), Ts: e.Timestamp.UnixMilli(), Iter: e.Iteration, ForkID: e.ForkID}
	switch e.Type {
	case event.EventModelChunk, event.EventReasoningChunk,
		event.EventStreamFallback, event.EventError,
		event.EventLoopEnd, event.EventIterationEnd:
		out.Data = raw(fmt.Sprint(e.Data))
	case event.EventLoopStart:
		out.Data = raw(e.Data)
	case event.EventToolStart:
		if c, ok := e.Data.(*types.ToolCall); ok {
			out.Data = raw(ToolStartData{ID: c.ID, Name: c.Name, Args: c.Args})
		}
	case event.EventToolChunk:
		out.Data = raw(e.Data) // types.ToolCallDelta 原样透传（building 态渲染）
	case event.EventToolEnd:
		if r, ok := e.Data.(*types.ToolResult); ok {
			d := ToolEndData{CallID: r.CallID, Name: r.Name, Content: r.Content}
			if r.Err != nil {
				d.Err = r.Err.Error()
			}
			out.Data = raw(d)
		}
	case event.EventModelEnd:
		if r, ok := e.Data.(*types.ModelResponse); ok {
			u := r.Usage
			d := ModelEndData{Content: r.Content, Reasoning: r.Reasoning, Usage: &u}
			for _, c := range r.ToolCalls {
				d.ToolCalls = append(d.ToolCalls, ToolStartID{ID: c.ID, Name: c.Name})
			}
			out.Data = raw(d)
		}
	case hooks.EventCompact:
		out.Data = raw(e.Data) // CompactInfo 原样透传
	case hooks.EventTrim, hooks.EventTrimming:
		out.Data = raw(e.Data) // TrimInfo / 提示文本原样透传
	case hooks.EventStatus:
		out.Data = raw(e.Data) // StatusData 原样透传
	case hooks.EventResChange:
		out.Data = raw(e.Data) // []string 变更条目原样透传（前端变更卡）
	case "task.start", "task.end":
		mapTaskEvent(&out, e)
	default:
		// approve.request / askuser.request：Data 恒 *types.ToolCall
		if c, ok := e.Data.(*types.ToolCall); ok {
			out.Data = raw(ToolStartData{ID: c.ID, Name: c.Name, Args: c.Args})
		}
	}
	return out
}

/* mapTaskEvent 处理 task 包的 start/end（避免 domain 依赖 task 的常量时用字符串）。 */
func mapTaskEvent(out *Event, e event.Event) {
	switch e.Type {
	case "task.start":
		if c, ok := e.Data.(*types.ToolCall); ok {
			var a struct {
				Task string `json:"task"`
			}
			_ = json.Unmarshal(c.Args, &a)
			out.Data = raw(TaskStartData{ID: e.ForkID, Task: a.Task, CallID: c.ID})
		}
	case "task.end":
		if s, ok := e.Data.(*types.LoopState); ok {
			answer := ""
			for i := len(s.Messages) - 1; i >= 0; i-- {
				if s.Messages[i].Role == types.RoleAssistant {
					answer = s.Messages[i].Content
					break
				}
			}
			out.Data = raw(TaskEndData{
				StopReason: string(s.StopReason),
				Iterations: s.Iteration,
				Answer:     answer,
			})
		}
	}
}

/*
	ReplaySync 构造 SSE 建连首帧：告知前端当前是否有运行中的轮——

turnActive=false 时前端据此复位 busy（轮在断线窗口内结束会错过
turn_end 而卡"运行中"），turnActive=true 时前端截断本地本轮块，
让随后的整轮回放帧干净重建（防 user/回复块重复）。
*/
func ReplaySync(turnActive bool) Event {
	type syncData struct {
		TurnActive bool `json:"turnActive"`
	}
	return Event{Type: "replay.sync", Ts: time.Now().UnixMilli(), Data: raw(syncData{TurnActive: turnActive})}
}

/* TurnEnd 构造合成的 turn_end 帧。 */
func TurnEnd(stop string, iterations int, usage *types.Usage, err error, elapsedMs int64) Event {
	d := TurnEndData{StopReason: stop, Iterations: iterations, ElapsedMs: elapsedMs, Usage: usage}
	if err != nil {
		d.Err = err.Error()
		if d.StopReason == "" {
			d.StopReason = "error"
		}
	}
	return Event{Type: "turn_end", Ts: time.Now().UnixMilli(), Data: raw(d)}
}

/* Raw 序列化为事件 Data 载荷（跨包构造事件用）。 */
func Raw(v any) json.RawMessage { return raw(v) }

func raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}
