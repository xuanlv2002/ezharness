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
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xuanlv2002/ezloop/types"
)

/* SysPrompt 持有当前会话的 system 两段式内容。 */
type SysPrompt struct {
	mu         sync.Mutex
	base       string
	summary    string
	identityFn func() string // 会话身份块（render 时求值：换代/承源后 ID 永远正确，不进落盘 base）
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

/*
	Set 整体替换（compact 创建新 session 时：重组 base + 摘要段；

身份段独立不受影响——换代后 identityFn 实时读新 ID）。
*/
func (s *SysPrompt) Set(base, summary string) {
	s.mu.Lock()
	s.base, s.summary = base, summary
	s.mu.Unlock()
}

/* SetIdentityFn 设置会话身份块（Assemble 时注入，读 domain.Session 实时 ID）。 */
func (s *SysPrompt) SetIdentityFn(fn func() string) {
	s.mu.Lock()
	s.identityFn = fn
	s.mu.Unlock()
}

/*
	SessionIdentityBlock 渲染会话身份块：ID + 存档绝对路径——上下文整理

（trim）折叠的早期对话仍完整保存在存档里，模型据此回忆。
*/
func SessionIdentityBlock(id string) string {
	wd, _ := os.Getwd() // 进程 cwd 即数据目录（启动时 chdir）
	p := filepath.ToSlash(filepath.Join(wd, SessionsDir, id, "session.json"))
	return "<session>\n" +
		"当前会话 ID：" + id + "\n" +
		"本会话存档：" + p + "\n" +
		"（这是本会话的完整历史档案：上下文整理折叠掉的早期对话仍完整保留在此文件中，" +
		"需要回忆本会话此前内容时读取它。）\n</session>"
}

/*
	render 拼接三段（base + 身份 + 摘要）；调用方须持 s.mu（不可重入锁，

render 自身不拿锁——Prompt/Parts 持锁后调用）。
*/
func (s *SysPrompt) render() string {
	parts := make([]string, 0, 3)
	if s.base != "" {
		parts = append(parts, s.base)
	}
	if s.identityFn != nil {
		if id := s.identityFn(); id != "" {
			parts = append(parts, id)
		}
	}
	if s.summary != "" {
		parts = append(parts, s.summary)
	}
	return strings.Join(parts, "\n\n")
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
