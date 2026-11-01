/*
ChatService 编排对话用例：发送、取消、决策回传与事件流消费。
按分支路由（rootID=线根 ID，稳定不随 compact 换代）：发送/取消/决策
作用于所属分支；轮由 Send 启动的 goroutine 消费完毕——后台分支照常
跑完，事件进各自 turnFrames，切回即重放。发送是打断式：目标正在跑轮
就先取消并等收尾，再启动新轮；归档中直接拒绝。
*/
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/ext/hook/mcp"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
)

/* sendInterruptWait 是打断式发送等待上一轮收尾的上限。 */
const sendInterruptWait = 8 * time.Second

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
turn_end。目标正在跑轮时先取消并在 8 秒内等收尾，再启动新轮。
*/
func (c *ChatService) Send(rootID, text string, refs []hooks.RefFile) error {
	return c.send(rootID, "", text, refs)
}

/*
SendSession 按创建任务时保存的叶 session ID 投递：除了 Send 的打断语义，
还要求该线仍然是同一个会话；compact 换代后返回 ErrSessionChanged，不会
把消息写到新会话。
*/
func (c *ChatService) SendSession(rootID, sessionID, text string, refs []hooks.RefFile) error {
	return c.send(rootID, sessionID, text, refs)
}

func (c *ChatService) send(rootID, expectedID, text string, refs []hooks.RefFile) error {
	if main := c.Hub.ModelsSnapshot().ActiveMain(); main == nil || main.APIKey == "" {
		return domain.ErrNoAPIKey
	}
	s := c.resolve(rootID)
	if s == nil {
		return ErrTopicNotFound
	}
	if expectedID != "" && s.CurrentID() != expectedID {
		return domain.ErrSessionChanged
	}
	if s.Archiving() {
		return domain.ErrBusy
	}
	if s.Busy() {
		s.Cancel()
		if !waitRunEnd(s, sendInterruptWait) {
			return fmt.Errorf("%w: 当前轮未能在 %s 内终止", domain.ErrBusy, sendInterruptWait)
		}
	}
	h, cancel, err := s.StartRunFor(expectedID, context.Background(), text, refs)
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
		// 轮失败进日志（错误只在 UI 呈现，远程排障没有落点）；用户主动取消不算
		if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
			log.Printf("ezharness: 轮失败（session %s）: %v", s.ID, waitErr)
		}
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
		modelName := ""
		if m := c.Hub.ModelsSnapshot().ActiveMain(); m != nil {
			modelName = m.Name
		}
		c.Hub.Stats.AddTurn(usage, modelName)
		recordUsage(c.Hub, usage) // 主模型条目用量累计并落盘
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
		elapsed := time.Since(started).Milliseconds()
		s.Publish(domain.TurnEnd(stop, iters, usage, waitErr, elapsed))
		// 轮末记一条完成通知（会话级只留最近一条，通知栏轮询取用，关闭即清）；
		// 主动取消不算完成（发起者自己知道，别刷通知）
		if !errors.Is(waitErr, context.Canceled) {
			s.MarkCompleted(fmt.Sprintf("%s · %d 轮 · %.1fs", hooks.FriendlyStop(stop), iters, float64(elapsed)/1000))
		}
	}()
	return nil
}

/* waitRunEnd 等被打断的轮收尾（FinishRun 释放 busy）。 */
func waitRunEnd(s *domain.Session, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for s.Busy() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
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
DecidePlan 回传计划确认处置：confirm 放行落盘，feedback 带意见（原样进
工具结果）。两种都记一条决策回执，轮末刷新后工具卡有徽标。
*/
func (c *ChatService) DecidePlan(rootID, callID, action, feedback string) {
	s := c.resolve(rootID)
	if s == nil {
		return
	}
	res := "已确认计划"
	if action == hooks.PlanActionFeedback {
		res = "已反馈"
		if fb := strings.TrimSpace(feedback); fb != "" {
			res = "已反馈：" + fb
		}
	}
	c.recordDecision(s, "plan", callID, res)
	s.DecidePlan(hooks.PlanDecision{CallID: callID, Action: action, Feedback: feedback})
}

/* DecideMcpAuth 回传 MCP 授权决策（授权完成后解除工具调用的阻塞并重试）。 */
func (c *ChatService) DecideMcpAuth(rootID, callID string, ok bool, reason string) {
	s := c.resolve(rootID)
	if s == nil {
		return
	}
	res := "已授权"
	if !ok {
		res = "已取消"
		if reason != "" {
			res = "已取消：" + reason
		}
	}
	c.recordDecision(s, "mcp_auth", callID, res)
	s.DecideMcpAuth(mcp.AuthDecision{CallID: callID, OK: ok, Err: reason})
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
