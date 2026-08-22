/*
ChatService 编排对话用例：发送、取消、决策回传与事件流消费。
*/
package service

import (
	"context"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/ext/hook/taskplan"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/domain"
)

/* ChatService 对话用例。 */
type ChatService struct {
	Hub *domain.Hub
}

/* Send 启动一轮异步运行：事件流扇出 SSE，结束更新历史并发 turn_end。 */
func (c *ChatService) Send(text string) error {
	if main := c.Hub.ModelsSnapshot().ActiveMain(); main == nil || main.APIKey == "" {
		return domain.ErrNoAPIKey
	}
	s := c.Hub.Active
	h, cancel, err := s.StartRun(context.Background(), text)
	if err != nil {
		return err
	}

	go func() {
		for ev := range h.Events() {
			if ev.Type == event.EventModelEnd {
				if r, ok := ev.Data.(*types.ModelResponse); ok {
					s.SetCtxTokens(r.Usage.PromptTokens)
				}
			}
			s.Publish(domain.MapEvent(ev))
		}
		state, waitErr := h.Wait()
		var usage *types.Usage
		stop, iters := "", 0
		if state != nil {
			usage = &state.Usage
			stop, iters = string(state.StopReason), state.Iteration
		}
		s.FinishRun(state, waitErr)
		cancel() // 释放 turnCtx（决策 select 的 Done 依赖）
		c.Hub.Stats.AddTurn(usage)
		c.Hub.RecordUsage(usage) // 主模型条目用量累计
		s.Publish(domain.TurnEnd(stop, iters, usage, waitErr))
	}()
	return nil
}

/* Cancel 取消当前轮。 */
func (c *ChatService) Cancel() { c.Hub.Active.Cancel() }

/* DecideApprove 回传审批决策。 */
func (c *ChatService) DecideApprove(callID string, approve bool, reason string) {
	c.Hub.Active.DecideApprove(approveDecision(callID, approve, reason))
}

/* DecideAnswer 回传提问回答。 */
func (c *ChatService) DecideAnswer(callID, input string) {
	c.Hub.Active.DecideAnswer(answerOf(callID, input))
}

/* DecidePlan 回传规划处置。 */
func (c *ChatService) DecidePlan(callID, kind, input string) {
	c.Hub.Active.DecidePlan(planDecision(callID, kind, input))
}

func approveDecision(callID string, ok bool, reason string) approve.Decision {
	return approve.Decision{CallID: callID, Approve: ok, Reason: reason}
}

func answerOf(callID, input string) askuser.Answer {
	return askuser.Answer{CallID: callID, Input: input}
}

func planDecision(callID, kind, input string) taskplan.Decision {
	d := taskplan.Decision{CallID: callID, Input: input}
	switch kind {
	case "execute":
		d.Kind = taskplan.Execute
	case "reject":
		d.Kind = taskplan.Reject
	default:
		d.Kind = taskplan.Revise
	}
	return d
}
