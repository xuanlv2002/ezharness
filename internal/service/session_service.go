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
	SessionID    string          `json:"sessionId"`
	Settings     domain.Settings `json:"settings"`
	Status       Status          `json:"status"`
	MemoryExists bool            `json:"memoryExists"`
}

/* Bootstrap 汇总启动数据。 */
func (s *SessionService) Bootstrap() BootstrapData {
	sess := s.Hub.Active
	return BootstrapData{
		SessionID:    sess.ID,
		Settings:     s.Hub.SettingsSnapshot(),
		Status:       s.Snapshot(),
		MemoryExists: memoryExists(s.Hub.Fsys),
	}
}

/* HistoryData 是历史响应。 */
type HistoryData struct {
	ID       string          `json:"id"`
	Busy     bool            `json:"busy"`
	Messages []types.Message `json:"messages"`
}

/* Status 是右栏生命体征数据。 */
type Status struct {
	Model           string   `json:"model"`
	SessionID       string   `json:"sessionId"`
	SessionMsgs     int      `json:"sessionMsgs"`
	Busy            bool     `json:"busy"`
	ContextTokens   int      `json:"contextTokens"`
	RotateThreshold int      `json:"rotateThreshold"`
	Tools           []string `json:"tools"`
	McpServers      []string `json:"mcpServers"`
	TopicsCount     int      `json:"topicsCount"`
}

/* Snapshot 汇总活动会话状态。 */
func (s *SessionService) Snapshot() Status {
	sess := s.Hub.Active
	st := s.Hub.SettingsSnapshot()
	w := sess.Wired()
	var tools []string
	if w != nil {
		tools = w.ToolNames
	}
	return Status{
		Model:           st.Model,
		SessionID:       sess.ID,
		SessionMsgs:     len(sess.History()),
		Busy:            sess.Busy(),
		ContextTokens:   sess.CtxTokens(),
		RotateThreshold: st.RotateThreshold,
		Tools:           tools,
		McpServers:      McpNames(s.Hub.Fsys),
		TopicsCount:     len(s.Hub.Topics.Load()),
	}
}

/* History 返回活动会话历史。 */
func (s *SessionService) History() HistoryData {
	sess := s.Hub.Active
	return HistoryData{ID: sess.ID, Busy: sess.Busy(), Messages: sess.History()}
}

/* Summarize 生成当前会话摘要（模型调用）。 */
func (s *SessionService) Summarize(ctx context.Context) (string, error) {
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
