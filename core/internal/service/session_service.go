/*
SessionService：启动装配数据（bootstrap）、状态快照、历史、按需摘要。
*/
package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/hook/skill"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/builtinskill"
	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
)

/* SessionService 会话查询用例。 */
type SessionService struct {
	Hub       *domain.Hub
	Callbacks *CallbackService // 可空：状态卡展示当前会话定时回调
	Agents    *AgentService    // 可空：切换会话模型时重建该会话装配
}

/* SessionPrefsPatch 是会话偏好的局部更新（nil = 不改）。 */
type SessionPrefsPatch struct {
	ReasoningEffort *string `json:"reasoningEffort"`
	ApproveMode     *string `json:"approveMode"`
	Model           *string `json:"model"`
}

/* SessionPrefsView 是更新后的会话偏好与解析出的主模型。 */
type SessionPrefsView struct {
	OK              bool   `json:"ok"`
	ReasoningEffort string `json:"reasoningEffort"`
	ApproveMode     string `json:"approveMode"`
	Model           string `json:"model"`
	ModelVision     bool   `json:"modelVision"`
	ContextWindow   int    `json:"contextWindow"`
}

/*
SetPrefs 更新本会话的思考档位/审批档位/主模型。模型变化需要会话空闲并
重建该会话装配（provider 与上下文窗口随模型走）；思考/审批由请求侧与
审批闭包实时读取，无需重建。偏好立即写入会话目录的 prefs.json，轮末
快照另存一份。
*/
func (s *SessionService) SetPrefs(ctx context.Context, rootID string, patch SessionPrefsPatch) (SessionPrefsView, error) {
	sess := s.Hub.SessionOf(rootID)
	if sess == nil {
		return SessionPrefsView{}, ErrTopicNotFound
	}
	p := sess.Sess.Prefs()
	modelChanged := false
	if patch.ReasoningEffort != nil {
		if !validReasoningEffort(*patch.ReasoningEffort) {
			return SessionPrefsView{}, fmt.Errorf("思考档位取值非法（空/none/low/medium/high/xhigh/max）")
		}
		p.ReasoningEffort = *patch.ReasoningEffort
	}
	if patch.ApproveMode != nil {
		if !validApproveMode(*patch.ApproveMode) {
			return SessionPrefsView{}, fmt.Errorf("审批档位取值非法（空/off/strict）")
		}
		p.ApproveMode = *patch.ApproveMode
	}
	if patch.Model != nil {
		name := strings.TrimSpace(*patch.Model)
		if name != "" {
			found := false
			for _, e := range s.Hub.ModelsSnapshot().Main {
				if e.Name == name && !e.Disabled {
					found = true
					break
				}
			}
			if !found {
				return SessionPrefsView{}, fmt.Errorf("模型 %s 不存在或已关闭", name)
			}
		}
		if name != p.ActiveModel {
			modelChanged = true
		}
		p.ActiveModel = name
	}
	if modelChanged && (sess.Busy() || sess.Archiving()) {
		return SessionPrefsView{}, domain.ErrBusy
	}
	sess.Sess.SetPrefs(p)
	if err := hooks.SavePrefs(ctx, s.Hub.Fsys, sess.ID, p); err != nil {
		return SessionPrefsView{}, err
	}
	if modelChanged && s.Agents != nil {
		s.Agents.Assemble(sess, s.Hub.SettingsSnapshot())
	}
	main := s.Hub.ModelsSnapshot().ResolveMain(p.ActiveModel)
	out := SessionPrefsView{OK: true, ReasoningEffort: p.ReasoningEffort, ApproveMode: p.ApproveMode}
	if main != nil {
		out.Model, out.ModelVision, out.ContextWindow = main.Name, main.Vision, main.ContextWindow
	}
	return out, nil
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
func (s *SessionService) Bootstrap(ctx context.Context) BootstrapData {
	sess := s.Hub.ActiveSession()
	st := s.Hub.SettingsSnapshot()
	p := st.TrimPercent
	w := st.WorkDir
	userItems, _ := hooks.LoadItems(ctx, s.Hub.Fsys, hooks.UserJson)
	soulItems, _ := hooks.LoadItems(ctx, s.Hub.Fsys, hooks.SoulJson)
	return BootstrapData{
		SessionID:    sess.RootID,
		LeafID:       sess.ID,
		Branches:     buildBranchViews(s.Hub),
		Settings:     SettingsView{SystemExtra: st.SystemExtra, TrimPercent: &p, WorkDir: &w},
		Status:       s.Snapshot(ctx),
		MemoryExists: len(userItems) > 0 || len(soulItems) > 0,
	}
}

/* HistoryData 是历史响应。 */
type HistoryData struct {
	ID         string                 `json:"id"` // 叶 session ID
	RootID     string                 `json:"rootId"`
	Busy       bool                   `json:"busy"`
	Messages   []types.Message        `json:"messages"`           // 剥 system：与盘上快照同基准（分叉 msgIdx 对位）
	Start      int                    `json:"start,omitempty"`    // tail 截取时的消息偏移（msgIdx = start + 下标）
	TargetID   string                 `json:"targetId,omitempty"` // 向上边目标（compress 链上翻游标）
	SeedKind   string                 `json:"seedKind,omitempty"` // new | fork | compress
	CanPrev    bool                   `json:"canPrev"`            // 上翻是否还有上一级（后端判定，fork 换源后算）
	ForkedFrom *hooks.ForkOrigin      `json:"forkedFrom,omitempty"`
	PrevTitle  string                 `json:"prevTitle,omitempty"` // 上一话题标题（压缩标记用）
	Decisions  []hooks.DecisionRecord `json:"decisions,omitempty"` // 人机决策记录（工具卡徽标用）
	Forks      []hooks.ForkSummary    `json:"forks,omitempty"`     // fork 分身摘要（入口卡重建，详情懒加载）
	Plan       *hooks.SessionPlan     `json:"plan,omitempty"`      // 分支计划（输入框上方进度面板恢复）
}

/* Status 是右栏状态卡数据（命中率与用量为本会话口径，切会话/重启清零）。 */
type Status struct {
	Model           string          `json:"model"`
	ModelVision     bool            `json:"modelVision"`     // 主模型是否支持视觉输入（false 时带图发送前端提示省略）
	ReasoningEffort string          `json:"reasoningEffort"` // 本会话思考档位（''=默认）
	ApproveMode     string          `json:"approveMode"`     // 本会话审批档位（''=跟随安全配置）
	SessionID       string          `json:"sessionId"`
	RootID          string          `json:"rootId"`
	SessionMsgs     int             `json:"sessionMsgs"`
	Busy            bool            `json:"busy"`
	ContextTokens   int             `json:"contextTokens"`
	ContextWindow   int             `json:"contextWindow"` // 主模型窗口（水位条分母）
	TrimPercent     int             `json:"trimPercent"`   // 自动整理阈值（窗口百分比，水位条阈值线；0=禁用）
	CacheHitRate    float64         `json:"cacheHitRate"`
	PromptTokens    int             `json:"promptTokens"` // 本会话累计输入（命中率分母）
	Turns           int             `json:"turns"`
	Tools           []string        `json:"tools"`
	McpServers      []string        `json:"mcpServers"`
	Skills          []string        `json:"skills"`
	TopicsCount     int             `json:"topicsCount"`
	Callbacks       []CallbackBrief `json:"callbacks,omitempty"` // 绑定当前会话的定时回调
}

/* Snapshot 汇总活动会话状态。 */
func (s *SessionService) Snapshot(ctx context.Context) Status {
	sess := s.Hub.ActiveSession()
	w := sess.Wired()
	var tools []string
	if w != nil {
		tools = w.ToolNames
	}
	if tools == nil {
		tools = []string{}
	}
	mcp := McpNames(ctx, s.Hub.Fsys)
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
	all := builtinskill.Skills()
	if entries, err := skill.LoadDir(ctx, s.Hub.Fsys, hooks.SkillsDir); err == nil {
		// 与 <skills> 段同一归置：内建优先，同名用户技能让位
		for _, e := range entries {
			if !slices.ContainsFunc(all, func(b skill.Skill) bool {
				return skill.DirOf(b.Path) == skill.DirOf(e.Path)
			}) {
				all = append(all, e)
			}
		}
	}
	disabled := s.Hub.SettingsSnapshot().DisabledSkills
	for _, e := range all {
		if slices.Contains(disabled, skill.DirOf(e.Path)) {
			continue
		}
		skills = append(skills, e.Name)
	}
	prefs := sess.Sess.Prefs()
	vision := false
	modelName := ""
	if m := s.Hub.ModelsSnapshot().ResolveMain(prefs.ActiveModel); m != nil {
		vision, modelName = m.Vision, m.Name
	}
	var callbacks []CallbackBrief
	if s.Callbacks != nil {
		callbacks = s.Callbacks.Brief(sess.CurrentID())
	}
	return Status{
		Model:           modelName,
		ModelVision:     vision,
		ReasoningEffort: prefs.ReasoningEffort,
		ApproveMode:     prefs.ApproveMode,
		SessionID:       sess.ID,
		RootID:          sess.RootID,
		SessionMsgs:     len(sess.History()),
		Busy:            sess.Busy(),
		ContextTokens:   ctxTokens,
		ContextWindow:   ctxWindow,
		TrimPercent:     s.Hub.SettingsSnapshot().TrimPercent,
		CacheHitRate:    hit,
		PromptTokens:    u.PromptTokens,
		Turns:           s.Hub.Stats.Turns(),
		Tools:           tools,
		McpServers:      mcp,
		Skills:          skills,
		TopicsCount:     len(s.Hub.Topics.Load()),
		Callbacks:       callbacks,
	}
}

/*
History 返回指定分支的当前历史（rootID 路由；未知分支返回空数据）。
tail>0 时只回尾部 tail 条消息，并跳过决策/分身/计划/上翻判定
（turn_end 回填锚点等只需要尾段的场景）。
*/
func (s *SessionService) History(ctx context.Context, rootID string, tail int) HistoryData {
	sess := s.Hub.SessionOf(rootID)
	if sess == nil {
		return HistoryData{}
	}
	messages := hooks.StripSystem(sess.History())
	start := 0
	if tail > 0 && len(messages) > tail {
		start = len(messages) - tail
		messages = messages[start:]
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
		ID:         sess.ID,
		RootID:     sess.RootID,
		Busy:       sess.Busy(),
		Messages:   messages,
		Start:      start,
		TargetID:   edge.TargetID,
		SeedKind:   edge.SeedKind,
		ForkedFrom: edge.ForkedFrom,
	}
	/* tail 模式只服务轮末锚点回填：决策/分身/计划/上翻判定都不参与，
	   不读这些档案（否则每轮都要扫全部已结束分身的 trace）。 */
	if tail <= 0 {
		h.CanPrev, h.PrevTitle = s.prevInfo(ctx, sess.ID)
		h.Decisions = hooks.LoadDecisions(ctx, sess.Fsys, h.ID)
		h.Forks = hooks.ListForks(ctx, sess.Fsys, h.ID)
		h.Plan, _ = hooks.LoadPlan(ctx, sess.Fsys, sess.RootID)
	}
	return h
}

/* prevInfo 判定 id 上翻是否还有上一级（与 Prev 同规则：fork 换源后算），
   并带回上一级标题供时间线的会话边界显示。 */
func (s *SessionService) prevInfo(ctx context.Context, id string) (bool, string) {
	cur, err := hooks.LoadSnapMeta(ctx, s.Hub.Fsys, id)
	if err != nil {
		return false, ""
	}
	if cur.SeedKind == "fork" && cur.TargetID != "" {
		src, serr := hooks.LoadSnapMeta(ctx, s.Hub.Fsys, cur.TargetID)
		if serr != nil {
			return false, ""
		}
		cur = src
	}
	if cur.TargetID == "" {
		return false, ""
	}
	prev, err := hooks.LoadSnapMeta(ctx, s.Hub.Fsys, cur.TargetID)
	if err != nil {
		return true, ""
	}
	return true, prev.Title
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
	cur, err := hooks.LoadSnapMeta(ctx, s.Hub.Fsys, id)
	if err != nil {
		return nil, false, err
	}
	if cur.SeedKind == "fork" && cur.TargetID != "" {
		src, serr := hooks.LoadSnapMeta(ctx, s.Hub.Fsys, cur.TargetID)
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
	/* 上一会话的摘要存在当前（子）会话的快照里；prev 自己的 CompactSummary
	   描述的是更上一代，不能拿来标这一代 */
	return &PrevData{ID: prev.ID, Title: title, Summary: cur.CompactSummary,
		Messages: prev.Messages, Forks: hooks.ListForks(ctx, s.Hub.Fsys, prev.ID),
		PrevSession: prev.TargetID}, true, nil
}
