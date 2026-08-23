/*
SessionService：启动装配数据（bootstrap）、状态快照、历史、按需摘要。
*/
package service

import (
	"context"
	"errors"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/hook/summary"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
)

/* ErrEmptySession 表示会话无消息可摘要。 */
var ErrEmptySession = errors.New("empty session")

/* SessionService 会话查询与摘要用例。 */
type SessionService struct {
	Hub *domain.Hub
}

/* BootstrapData 是前端启动所需的全量数据。 */
type BootstrapData struct {
	SessionID    string       `json:"sessionId"`
	Settings     SettingsView `json:"settings"`
	Status       Status       `json:"status"`
	MemoryExists bool         `json:"memoryExists"`
}

/* Bootstrap 汇总启动数据。 */
func (s *SessionService) Bootstrap() BootstrapData {
	sess := s.Hub.Active
	st := s.Hub.SettingsSnapshot()
	p := st.CompactThreshold
	return BootstrapData{
		SessionID:    sess.ID,
		Settings:     SettingsView{SystemExtra: st.SystemExtra, CompactThreshold: &p},
		Status:       s.Snapshot(),
		MemoryExists: memoryExists(s.Hub.Fsys),
	}
}

/* HistoryData 是历史响应。 */
type HistoryData struct {
	ID          string                    `json:"id"`
	Busy        bool                      `json:"busy"`
	Messages    []types.Message           `json:"messages"`
	PrevSession string                    `json:"prevSession,omitempty"` // compact 链上一会话（懒加载用）
	PrevTitle   string                    `json:"prevTitle,omitempty"`   // 上一话题标题（压缩标记用）
	Decisions   []hooks.DecisionRecord    `json:"decisions,omitempty"`   // 人机决策记录（工具卡徽标用）
}

/* Status 是右栏生命体征数据。 */
type Status struct {
	Model         string   `json:"model"`
	SessionID     string   `json:"sessionId"`
	SessionMsgs   int      `json:"sessionMsgs"`
	Busy          bool     `json:"busy"`
	ContextTokens int      `json:"contextTokens"`
	DaysServed    int      `json:"daysServed"`
	CacheHitRate  float64  `json:"cacheHitRate"`
	TotalTokens   int      `json:"totalTokens"`
	Turns         int      `json:"turns"`
	Tools         []string `json:"tools"`
	McpServers    []string `json:"mcpServers"`
	TopicsCount   int      `json:"topicsCount"`
}

/* mainModelName 返回主模型名（空槽显示空）。 */
func mainModelName(h *domain.Hub) string {
	if m := h.ModelsSnapshot().ActiveMain(); m != nil {
		return m.Name
	}
	return ""
}

/* Snapshot 汇总活动会话状态。 */
func (s *SessionService) Snapshot() Status {
	sess := s.Hub.Active
	w := sess.Wired()
	var tools []string
	if w != nil {
		tools = w.ToolNames
	}
	total := s.Hub.Stats.Total()
	return Status{
		Model:         mainModelName(s.Hub),
		SessionID:     sess.ID,
		SessionMsgs:   len(sess.History()),
		Busy:          sess.Busy(),
		ContextTokens: sess.CtxTokens(),
		DaysServed:    s.Hub.Stats.DaysServed(),
		CacheHitRate:  s.Hub.Stats.CacheHitRate(),
		TotalTokens:   total.PromptTokens + total.CompletionTokens,
		Turns:         s.Hub.Stats.Turns(),
		Tools:         tools,
		McpServers:    McpNames(s.Hub.Fsys),
		TopicsCount:   len(s.Hub.Topics.Load()),
	}
}

/* History 返回活动会话历史。 */
func (s *SessionService) History() HistoryData {
	sess := s.Hub.Active
	h := HistoryData{ID: sess.ID, Busy: sess.Busy(), Messages: sess.History(), PrevSession: sess.Sess.PrevID()}
	if h.PrevSession != "" {
		for _, e := range s.Hub.Topics.Load() {
			if e.ID == h.PrevSession {
				h.PrevTitle = e.Title
				break
			}
		}
	}
	h.Decisions = hooks.LoadDecisions(context.Background(), s.Hub.Active.Fsys, h.ID)
	return h
}

/* PrevData 是懒加载上一会话响应。 */
type PrevData struct {
	ID          string          `json:"id"`
	Title       string          `json:"title,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Messages    []types.Message `json:"messages"`
	PrevSession string          `json:"prevSession,omitempty"` // 再上一级 ID（非空可继续上翻）
}

/*
Prev 沿 compact 链取 id 的上一会话内容（向上滚动懒加载）。id 允许
链上任一会话（读归档只读安全）；无上一级返回 ok=false。
*/
func (s *SessionService) Prev(ctx context.Context, id string) (*PrevData, bool, error) {
	cur, err := hooks.LoadSnap(ctx, s.Hub.Fsys, id)
	if err != nil {
		return nil, false, err
	}
	if cur.PrevSession == "" {
		return nil, false, nil
	}
	prev, err := hooks.LoadSnap(ctx, s.Hub.Fsys, cur.PrevSession)
	if err != nil {
		return nil, false, err
	}
	title, summary := "", prev.CompactSummary
	for _, e := range s.Hub.Topics.Load() {
		if e.ID == cur.PrevSession {
			title, summary = e.Title, e.Summary
			break
		}
	}
	return &PrevData{ID: prev.ID, Title: title, Summary: summary,
		Messages: prev.Messages, PrevSession: prev.PrevSession}, true, nil
}

/* Summarize 生成当前会话摘要（模型调用）。 */
func (s *SessionService) Summarize(ctx context.Context) (string, error) {
	if main := s.Hub.ModelsSnapshot().ActiveMain(); main == nil || main.APIKey == "" {
		return "", domain.ErrNoAPIKey
	}
	sess := s.Hub.Active
	hist := sess.History()
	if len(hist) == 0 {
		return "", ErrEmptySession
	}
	w := sess.Wired()
	if w == nil {
		return "", ErrEmptySession
	}
	return summary.Summarize(ctx, w.Provider, hist, "")
}

func memoryExists(fsys interface {
	Read(ctx context.Context, path string) ([]byte, error)
}) bool {
	data, err := fsys.Read(context.Background(), hooks.MemoryFile)
	return err == nil && len(strings.TrimSpace(string(data))) > 0
}
