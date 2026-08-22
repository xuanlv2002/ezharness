/*
sysprompt 是 system 消息的唯一来源：替代 core.WithSystemPrompt——
agent 构建后只读是引擎契约，compact 需要运行中更换 system，故由
StartHook 每轮注入可变 prompt。base（人格+记忆+skill/mcp 列表）与
summary（compact 摘要段）在 session 创建时固定，重启恢复从快照还原，
不重新组装。装配必须排 startHooks 首位（先有 system，后续 hook 再动
消息列表）。fork 不受影响：startHooks 不重跑，seed 已含 system。
*/
package hooks

import (
	"context"
	"sync"

	"github.com/xuanlv2002/ezloop/types"
)

/* SysPrompt 持有当前会话的 system 两段式内容。 */
type SysPrompt struct {
	mu      sync.Mutex
	base    string
	summary string
}

/* NewSysPrompt 创建（base 必有，summary 是 compact 产物可为空）。 */
func NewSysPrompt(base, summary string) *SysPrompt {
	return &SysPrompt{base: base, summary: summary}
}

/* Prompt 返回渲染后的完整 system。 */
func (s *SysPrompt) Prompt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.render()
}

/* Parts 返回 base 与 summary 两段。 */
func (s *SysPrompt) Parts() (base, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base, s.summary
}

/* Set 整体替换（compact 创建新 session 时：重组 base + 摘要段）。 */
func (s *SysPrompt) Set(base, summary string) {
	s.mu.Lock()
	s.base, s.summary = base, summary
	s.mu.Unlock()
}

func (s *SysPrompt) render() string {
	if s.summary == "" {
		return s.base
	}
	return s.base + "\n\n" + s.summary
}

func (s *SysPrompt) Name() string { return "sysprompt" }

/* OnStart 在消息序列缺 system 时注入当前 prompt（全程唯一一条）。 */
func (s *SysPrompt) OnStart(_ context.Context, state *types.LoopState) error {
	if len(state.Messages) > 0 && state.Messages[0].Role == types.RoleSystem {
		state.Messages[0].Content = s.Prompt()
		return nil
	}
	state.Messages = append([]types.Message{{
		Role:    types.RoleSystem,
		Content: s.Prompt(),
	}}, state.Messages...)
	return nil
}
