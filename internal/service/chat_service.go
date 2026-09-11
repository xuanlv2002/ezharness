/*
ChatService 编排对话用例：发送、取消、决策回传与事件流消费。
按分支路由（rootID=线根 ID，稳定不随 compact 换代）：发送/取消/决策
作用于所属分支；轮由 Send 启动的 goroutine 消费完毕——后台分支照常
跑完，事件进各自 turnFrames，切回即重放。
*/
package service

import (
	"context"
	"time"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
)

/* ChatService 对话用例。 */
type ChatService struct {
	Hub *domain.Hub
}

/* resolve 按线根 ID 定位分支（nil = 未加载分支）。 */
func (c *ChatService) resolve(rootID string) *domain.Session {
	return c.Hub.SessionOf(rootID)
}

/*
	Send 启动一轮异步运行：引用为工作目录内路径的结构化列表（附件 =

整文件引用，文件页标注 = 片段+行号+备注；表现层已校验归属），统一经
<reference_file> 记录告知模型。事件流扇出 SSE，结束更新历史并发
turn_end。
*/
func (c *ChatService) Send(rootID, text string, refs []hooks.RefFile) error {
	if main := c.Hub.ModelsSnapshot().ActiveMain(); main == nil || main.APIKey == "" {
		return domain.ErrNoAPIKey
	}
	s := c.resolve(rootID)
	if s == nil {
		return ErrTopicNotFound
	}
	h, cancel, err := s.StartRun(context.Background(), text, refs)
	if err != nil {
		return err
	}
	// 首次发言落线索引（NewBranch 延迟建索引防空线粉尘；fork 线创建时已索引）
	if _, ok := c.Hub.Topics.Get(s.RootID); !ok {
		now := time.Now().UnixMilli()
		_ = c.Hub.Topics.Add(hooks.TopicEntry{
			ID: s.RootID, LeafID: s.ID,
			Title:     hooks.FirstUserTitle([]types.Message{{Role: types.RoleUser, Content: text}}),
			Kind:      "new",
			CreatedAt: now, UpdatedAt: now, Msgs: len(s.History()),
		})
	}

	go func() {
		started := time.Now()
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
		if stop == "" && waitErr != nil {
			stop = "error" // 与 TurnEnd 帧同口径
		}
		s.FinishRun(state, waitErr)
		cancel() // 释放 turnCtx（决策 select 的 Done 依赖）
		c.Hub.Stats.AddTurn(usage)
		c.Hub.RecordUsage(usage) // 主模型条目用量累计
		// 轮末刷新线（叶子/活动时间/规模）；未命名线按本代首条真实 user 命名
		if entry, ok := c.Hub.Topics.Get(s.RootID); ok {
			msgs := s.History()
			if entry.Title == "" || entry.Title == "未命名" {
				if t := hooks.FirstUserTitle(msgs); t != "未命名话题" {
					c.Hub.Topics.SetTitle(s.RootID, t)
				}
			}
			_ = c.Hub.Topics.UpdateLeaf(s.RootID, s.ID, "", time.Now().UnixMilli(), len(msgs))
		}
		s.Publish(domain.TurnEnd(stop, iters, usage, waitErr, time.Since(started).Milliseconds()))
	}()
	return nil
}

/* Cancel 取消当前轮。 */
func (c *ChatService) Cancel(rootID string) {
	if s := c.resolve(rootID); s != nil {
		s.Cancel()
	}
}

/* DecideApprove 回传审批决策。 */
func (c *ChatService) DecideApprove(rootID, callID string, ok bool, reason string) {
	s := c.resolve(rootID)
	if s == nil {
		return
	}
	res := "已批准"
	if !ok {
		res = "已拒绝"
		if reason != "" {
			res = "已拒绝：" + reason
		}
	}
	c.recordDecision(s, "approve", callID, res)
	s.DecideApprove(approve.Decision{CallID: callID, Approve: ok, Reason: reason})
}

/* DecideAnswer 回传提问回答。 */
func (c *ChatService) DecideAnswer(rootID, callID, input string) {
	s := c.resolve(rootID)
	if s == nil {
		return
	}
	c.recordDecision(s, "ask", callID, input)
	s.DecideAnswer(askuser.Answer{CallID: callID, Input: input})
}

/*
	recordDecision 持久化决策记录（轮末刷新后工具卡徽标用）并发

decision.resolved 帧（轮内刷新回放时纠正决策卡与徽标）。失败静默。
*/
func (c *ChatService) recordDecision(s *domain.Session, kind, callID, resolution string) {
	if callID == "" {
		return
	}
	hooks.AppendDecision(context.Background(), s.Fsys, s.ID, hooks.DecisionRecord{
		CallID: callID, Kind: kind, Resolution: resolution, Ts: time.Now().UnixMilli(),
	})
	s.Publish(domain.Event{Type: "decision.resolved", Data: domain.Raw(
		map[string]string{"id": callID, "resolution": resolution})})
}
