/*
SessionService：启动装配数据（bootstrap）、状态快照、历史、按需摘要。
*/
package service

import (
	"context"
	"slices"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/hook/skill"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
)

/* SessionService 会话查询用例。 */
type SessionService struct {
	Hub *domain.Hub
}

/* BootstrapData 是前端启动所需的全量数据。 */
type BootstrapData struct {
	SessionID    string       `json:"sessionId"` // = 分支根 ID（前端路由/SSE 订阅键，稳定不随 compact 换代）
	LeafID       string       `json:"leafId"`    // 当前叶 session ID
	Branches     []BranchView `json:"branches"`
	Settings     SettingsView `json:"settings"`
	Status       Status       `json:"status"`
	MemoryExists bool         `json:"memoryExists"`
}

/* Bootstrap 汇总启动数据。 */
func (s *SessionService) Bootstrap() BootstrapData {
	sess := s.Hub.Active
	st := s.Hub.SettingsSnapshot()
	p := st.TrimPercent
	w := st.WorkDir
	memData, memErr := s.Hub.Fsys.Read(context.Background(), hooks.HarnessMd)
	return BootstrapData{
		SessionID:    sess.RootID,
		LeafID:       sess.ID,
		Branches:     buildBranchViews(s.Hub),
		Settings:     SettingsView{SystemExtra: st.SystemExtra, TrimPercent: &p, WorkDir: &w},
		Status:       s.Snapshot(),
		MemoryExists: memErr == nil && len(strings.TrimSpace(string(memData))) > 0,
	}
}

/* HistoryData 是历史响应。 */
type HistoryData struct {
	ID          string                 `json:"id"` // 叶 session ID
	RootID      string                 `json:"rootId"`
	Busy        bool                   `json:"busy"`
	Messages    []types.Message        `json:"messages"` // 剥 system：与盘上快照同基准（分叉 msgIdx 对位）
	TargetID    string                 `json:"targetId,omitempty"`  // 向上边目标（compress 链上翻游标）
	SeedKind    string                 `json:"seedKind,omitempty"` // new | fork | compress
	CanPrev     bool                   `json:"canPrev"`             // 上翻是否还有上一级（后端判定，fork 换源后算）
	ForkedFrom  *hooks.ForkOrigin      `json:"forkedFrom,omitempty"`
	PrevTitle   string                 `json:"prevTitle,omitempty"` // 上一话题标题（压缩标记用）
	Decisions   []hooks.DecisionRecord `json:"decisions,omitempty"` // 人机决策记录（工具卡徽标用）
	Forks       []hooks.ForkSummary    `json:"forks,omitempty"`     // fork 分身摘要（入口卡重建，详情懒加载）
}

/* Status 是右栏状态卡数据（命中率与用量为本会话口径，切会话/重启清零）。 */
type Status struct {
	Model            string   `json:"model"`
	ModelVision      bool     `json:"modelVision"` // 主模型是否支持视觉输入（false 时带图发送前端提示省略）
	SessionID        string   `json:"sessionId"`
	RootID           string   `json:"rootId"`
	SessionMsgs      int      `json:"sessionMsgs"`
	Busy             bool     `json:"busy"`
	ContextTokens    int      `json:"contextTokens"`
	ContextWindow    int      `json:"contextWindow"` // 主模型窗口（水位条分母）
	TrimPercent      int      `json:"trimPercent"`   // 自动整理阈值（窗口百分比，水位条阈值线；0=禁用）
	CacheHitRate     float64  `json:"cacheHitRate"`
	PromptTokens     int      `json:"promptTokens"` // 本会话累计输入（命中率分母）
	Turns            int      `json:"turns"`
	Tools            []string `json:"tools"`
	McpServers       []string `json:"mcpServers"`
	Skills           []string `json:"skills"`
	TopicsCount      int      `json:"topicsCount"`
}

/* Snapshot 汇总活动会话状态。 */
func (s *SessionService) Snapshot() Status {
	sess := s.Hub.Active
	w := sess.Wired()
	var tools []string
	if w != nil {
		tools = w.ToolNames
	}
	if tools == nil {
		tools = []string{}
	}
	mcp := McpNames(s.Hub.Fsys)
	if mcp == nil {
		mcp = []string{}
	}
	u := sess.Sess.Usage()
	hit := 0.0
	if u.PromptTokens > 0 {
		hit = float64(u.CachedTokens) / float64(u.PromptTokens)
	}
	ctxTokens, ctxWindow := sess.Sess.CtxInfo()
	skills := []string{}
	if entries, err := skill.LoadDir(context.Background(), s.Hub.Fsys, hooks.SkillsDir); err == nil {
		disabled := s.Hub.SettingsSnapshot().DisabledSkills
		for _, e := range entries {
			if slices.Contains(disabled, hooks.SkillDirOf(e.Path)) {
				continue
			}
			skills = append(skills, e.Name)
		}
	}
	vision := false
	modelName := ""
	if m := s.Hub.ModelsSnapshot().ActiveMain(); m != nil {
		vision, modelName = m.Vision, m.Name
	}
	return Status{
		Model:            modelName,
		ModelVision:      vision,
		SessionID:        sess.ID,
		RootID:           sess.RootID,
		SessionMsgs:      len(sess.History()),
		Busy:             sess.Busy(),
		ContextTokens:    ctxTokens,
		ContextWindow:    ctxWindow,
		TrimPercent:   s.Hub.SettingsSnapshot().TrimPercent,
		CacheHitRate:     hit,
		PromptTokens:     u.PromptTokens,
		Turns:            s.Hub.Stats.Turns(),
		Tools:            tools,
		McpServers:       mcp,
		Skills:           skills,
		TopicsCount:      len(s.Hub.Topics.Load()),
	}
}

/* History 返回指定分支的当前历史（rootID 路由；未知分支返回空数据）。 */
func (s *SessionService) History(rootID string) HistoryData {
	sess := s.Hub.SessionOf(rootID)
	if sess == nil {
		return HistoryData{}
	}
	edge := sess.Sess.Edge()
	if edge.TargetID == "" {
		// compact 换库后 SetID 清空了 edge，compress 边只在 prevID 里
		//（重启/下次落盘才回填 edge）——上翻游标以 prevID 兜底
		if prev := sess.Sess.PrevID(); prev != "" {
			edge.TargetID, edge.SeedKind = prev, "compress"
		}
	}
	h := HistoryData{
		ID:      sess.ID,
		RootID:  sess.RootID,
		Busy:    sess.Busy(),
		Messages: hooks.StripSystem(sess.History()),
		TargetID: edge.TargetID,
		SeedKind: edge.SeedKind,
		CanPrev:  s.canPrev(context.Background(), sess.ID),
		ForkedFrom: edge.ForkedFrom,
	}
	h.Decisions = hooks.LoadDecisions(context.Background(), sess.Fsys, h.ID)
	h.Forks = hooks.ListForks(context.Background(), sess.Fsys, h.ID)
	return h
}

/* canPrev 判定 id 上翻是否还有上一级（与 Prev 同规则：fork 换源后算）。 */
func (s *SessionService) canPrev(ctx context.Context, id string) bool {
	cur, err := hooks.LoadSnap(ctx, s.Hub.Fsys, id)
	if err != nil {
		return false
	}
	if cur.SeedKind == "fork" && cur.TargetID != "" {
		src, serr := hooks.LoadSnap(ctx, s.Hub.Fsys, cur.TargetID)
		if serr != nil {
			return false
		}
		cur = src
	}
	return cur.TargetID != ""
}

/* ForkData 是 fork 分身详情响应（抽屉懒加载）。 */
type ForkData struct {
	ID        string                 `json:"id"`
	Messages  []types.Message        `json:"messages"`
	Decisions []hooks.DecisionRecord `json:"decisions,omitempty"` // 主库决策记录（前端按 callId 匹配）
}

/* Fork 返回 fork 分身增量消息（存档均为已结束分身；运行中靠实时事件）。 */
func (s *SessionService) Fork(ctx context.Context, id, fid string) (*ForkData, error) {
	snap, err := hooks.LoadFork(ctx, s.Hub.Fsys, id, fid)
	if err != nil {
		return nil, err
	}
	return &ForkData{
		ID:        snap.ID,
		Messages:  snap.Messages,
		Decisions: hooks.LoadDecisions(ctx, s.Hub.Fsys, id),
	}, nil
}

/* PrevData 是懒加载上一会话响应。 */
type PrevData struct {
	ID          string              `json:"id"`
	Title       string              `json:"title,omitempty"`
	Summary     string              `json:"summary,omitempty"`
	Messages    []types.Message     `json:"messages"`
	Forks       []hooks.ForkSummary `json:"forks,omitempty"`       // 旧库的分身摘要（入口卡重建）
	PrevSession string              `json:"prevSession,omitempty"` // 再上一级 ID（compress 链游标，非空可继续上翻）
}

/*
Prev 沿向上边取 id 的上一会话内容（向上滚动懒加载）。id 允许
链上任一会话（读归档只读安全）。fork 会话体内已含源 [0,anchor]
前缀副本——上翻从源的上一级继续（源的更早世代对 fork 可见）；
源无上级则到底。
*/
func (s *SessionService) Prev(ctx context.Context, id string) (*PrevData, bool, error) {
	cur, err := hooks.LoadSnap(ctx, s.Hub.Fsys, id)
	if err != nil {
		return nil, false, err
	}
	if cur.SeedKind == "fork" && cur.TargetID != "" {
		src, serr := hooks.LoadSnap(ctx, s.Hub.Fsys, cur.TargetID)
		if serr != nil {
			return nil, false, nil
		}
		cur = src // 源前缀已在 fork 体内，上翻从源的上级开始
	}
	if cur.TargetID == "" {
		return nil, false, nil
	}
	prev, err := hooks.LoadSnap(ctx, s.Hub.Fsys, cur.TargetID)
	if err != nil {
		return nil, false, err
	}
	title := hooks.FirstUserTitle(prev.Messages)
	return &PrevData{ID: prev.ID, Title: title, Summary: prev.CompactSummary,
		Messages: prev.Messages, Forks: hooks.ListForks(ctx, s.Hub.Fsys, prev.ID),
		PrevSession: prev.TargetID}, true, nil
}
