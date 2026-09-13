/*
TopicService：分支（线）用例——新建、分叉（copy）、切换、列表、删除、回顾。
数据模型见会话树设计：session=节点（sessions/<id>/session.json），分支=
叶子到根的 compress 链，侧栏显示分支不显示会话；fork 从任意消息位置
复制前缀（含选中消息）开新线；归档世代继续聊走 fork@tip，同 ID 续写废弃。
*/
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
)

/* ErrTopicNotFound 话题/分支存档不存在。 */
var ErrTopicNotFound = errors.New("topic not found")

/* ErrBadAnchor fork 锚点越界（不在目标会话消息范围内）。 */
var ErrBadAnchor = errors.New("fork anchor out of range")

/* ErrCantArchive 活动/运行中的会话不可归档（会继续写入，标记会被覆盖）。 */
var ErrCantArchive = errors.New("活动或运行中的分支不可归档")

/* TopicService 分支用例。 */
type TopicService struct {
	Hub    *domain.Hub
	Agents *AgentService // 装配新分支的 agent（main 注入）
}

/* BranchView 是分支面板条目（索引 + 运行态合成）。 */
type BranchView struct {
	hooks.TopicEntry
	Running   bool `json:"running"`   // 有轮运行中（后台分支也亮）
	Waiting   bool `json:"waiting"`   // 有未决审批/提问
	Archiving bool `json:"archiving"` // 归档进行中（摘要期间锁该分支对话）
	Active    bool `json:"active"`    // 当前所处分支
}

/* buildBranchViews 由索引+注册表合成分支列表（含未索引的活动新分支）。 */
func buildBranchViews(h *domain.Hub) []BranchView {
	active := h.Active
	entries := h.Topics.Load()
	out := make([]BranchView, 0, len(entries)+1)
	found := false
	for _, e := range entries {
		v := BranchView{TopicEntry: e}
		if active != nil && e.ID == active.RootID {
			v.Active, found = true, true
			v.LeafID = active.ID // 内存态可能更新（compact 刚换代）
		}
		if s := h.SessionOf(e.ID); s != nil {
			v.Running = s.Busy()
			v.Waiting = s.PendingCount() > 0
			v.Archiving = s.Archiving()
		}
		out = append(out, v)
	}
	if active != nil && active.RootID != "" && !found {
		// 新建未发言的分支未入索引：合成置顶条目
		out = append(out, BranchView{TopicEntry: hooks.TopicEntry{
			ID: active.RootID, LeafID: active.ID, Title: "新对话", Kind: "new",
			CreatedAt: time.Now().UnixMilli(), Msgs: len(active.History()),
		}, Active: true})
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := out[i].UpdatedAt, out[j].UpdatedAt
		if ti == 0 {
			ti = out[i].CreatedAt
		}
		if tj == 0 {
			tj = out[j].CreatedAt
		}
		return ti > tj
	})
	return out
}

/* List 返回分支列表（侧栏/记忆页数据源）。 */
func (t *TopicService) List() []BranchView { return buildBranchViews(t.Hub) }

/* TopicDetail 是话题完整存档（只读回顾）。 */
type TopicDetail struct {
	Entry    hooks.TopicEntry `json:"entry"`
	Messages []types.Message  `json:"messages"`
	Summary  string           `json:"summary,omitempty"` // compact 摘要段（压缩新叶几乎无消息，回顾靠它给上下文）
}

/*
Get 读取分支完整存档（读 LeafID 快照；id 也允许是任意 session ID，
供分叉源会话/归档世代的只读回顾）。
*/
func (t *TopicService) Get(ctx context.Context, id string) (TopicDetail, error) {
	if entry, ok := t.Hub.Topics.Get(id); ok {
		leaf := entry.LeafID
		if leaf == "" {
			leaf = id
		}
		if snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, leaf); err == nil {
			return TopicDetail{Entry: entry, Messages: snap.Messages, Summary: snap.SummaryBlock}, nil
		}
	}
	if snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, id); err == nil {
		return TopicDetail{
			Entry:    hooks.TopicEntry{ID: id, Title: hooks.FirstUserTitle(snap.Messages), Msgs: len(snap.Messages)},
			Messages: snap.Messages,
			Summary:  snap.SummaryBlock,
		}, nil
	}
	return TopicDetail{}, ErrTopicNotFound
}

/*
NewBranch 开新线：新 session（SeedKind=new，挂空根）注册并切为活动分支。
索引延迟到首次发言（Send 时落），避免空线粉尘。
*/
func (t *TopicService) NewBranch() *domain.Session {
	s := t.Hub.CreateBranch()
	t.Agents.Assemble(s, t.Hub.SettingsSnapshot())
	t.Hub.SetActive(s)
	return s
}

/*
Fork 从任意消息位置复制前缀开新分支（copy 语义，快照隔离）：
新 session 体内携带目标 [0, anchor] 的完整副本（含选中消息），
system/水位承源；分叉出处仅存展示元数据，删源不伤内容。

空叉链折叠：源是"未产生新消息的 fork"（体量仍等于复制前缀）时，
选中消息的真实出处是更上游——沿 fork 边上溯到实际内容来源再建边，
避免 A→B(空)→C 显示成"来自 B"的空壳链。
标题取源线在索引里的标题（区分 fork-of-fork），退化首条 user。
*/
func (t *TopicService) Fork(ctx context.Context, sourceID string, anchor int) (*domain.Session, error) {
	src, err := hooks.LoadSnap(ctx, t.Hub.Fsys, sourceID)
	if err != nil {
		return nil, ErrTopicNotFound
	}
	if anchor <= 0 || anchor > len(src.Messages) {
		return nil, ErrBadAnchor
	}
	for src.SeedKind == "fork" && len(src.Messages) == src.Anchor && src.TargetID != "" {
		parent, perr := hooks.LoadSnap(ctx, t.Hub.Fsys, src.TargetID)
		if perr != nil {
			break
		}
		src = parent // 副本 [0, src.Anchor) 与 parent 同源，anchor 索引不变
	}
	title := hooks.TitleFromMsg(src.Messages[anchor-1]) // fork 用分叉锚点消息内容命名
	// 来源标注 = 源 session 名称（非分支名；记忆页树展示"来自 X"）
	srcTitle := src.Title
	if srcTitle == "" {
		srcTitle = hooks.FirstUserTitle(src.Messages)
	}
	newID := hooks.NewSessionID()
	now := time.Now().UnixMilli()
	snap := &hooks.SessionSnap{
		ID:           newID,
		CreatedAt:    now,
		Title:        title,
		Messages:     append([]types.Message(nil), src.Messages[:anchor]...),
		SystemPrompt: src.SystemPrompt,
		SystemBase:   src.SystemBase,
		SummaryBlock: src.SummaryBlock,
		Model:        src.Model,
		TargetID:     src.ID,
		Anchor:       anchor,
		SeedKind:     "fork",
		LineRoot:     newID,
		ForkedFrom:   &hooks.ForkOrigin{SourceID: src.ID, Title: srcTitle, Anchor: anchor},
		CtxTokens:    src.CtxTokens,
		CtxWindow:    src.CtxWindow,
	}
	if err := hooks.SaveSnap(ctx, t.Hub.Fsys, snap); err != nil {
		return nil, err
	}
	_ = t.Hub.Topics.Add(hooks.TopicEntry{
		ID: newID, LeafID: newID, Title: title, Kind: "fork",
		CreatedAt: now, UpdatedAt: now, Msgs: len(snap.Messages), Origin: snap.ForkedFrom,
	})
	s := t.Hub.LoadBranch(newID, snap)
	t.Agents.Assemble(s, t.Hub.SettingsSnapshot())
	t.Hub.SetActive(s)
	return s, nil
}

/*
Switch 切换分支：注册表命中直接切（后台轮不取消——阶段一线间并发，
切回时 ReplayFrames 重建时间线与未决审批）；未加载则按索引 LeafID
恢复。无 Busy 拒绝。
*/
func (t *TopicService) Switch(ctx context.Context, rootID string) error {
	if s := t.Hub.SessionOf(rootID); s != nil {
		t.Hub.SetActive(s)
		return nil
	}
	entry, ok := t.Hub.Topics.Get(rootID)
	if !ok {
		return ErrTopicNotFound
	}
	leaf := entry.LeafID
	if leaf == "" {
		leaf = rootID
	}
	snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, leaf)
	if err != nil {
		return ErrTopicNotFound
	}
	s := t.Hub.LoadBranch(rootID, snap)
	t.Agents.Assemble(s, t.Hub.SettingsSnapshot())
	t.Hub.SetActive(s)
	return nil
}

/*
Delete 删除分支：清线上全部世代目录（LineRoot 归属）+ 索引条目 + 注册表。
运行中拒绝；删活动分支时先换到全新分支。
*/
func (t *TopicService) Delete(ctx context.Context, rootID string) error {
	if s := t.Hub.SessionOf(rootID); s != nil {
		if s.Busy() {
			return domain.ErrBusy
		}
	}
	active := t.Hub.Active
	if active != nil && active.RootID == rootID {
		t.NewBranch() // 活动分支被删：先切到全新空线
	}
	t.Hub.Unregister(rootID)
	ids, _ := hooks.ListMain(ctx, t.Hub.Fsys)
	for _, id := range ids {
		if id == rootID {
			continue
		}
		if snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, id); err == nil && snap.LineRoot == rootID {
			_ = os.RemoveAll(filepath.Join(hooks.SessionsDir, id))
		}
	}
	_ = os.RemoveAll(filepath.Join(hooks.SessionsDir, rootID))
	t.Hub.Topics.Remove(rootID)
	return nil
}

/* SessionNode 是会话树节点（记忆页整树渲染：全部世代与分叉）。 */
type SessionNode struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	SeedKind     string            `json:"seedKind,omitempty"` // new | fork | compress
	Archived     bool              `json:"archived"`           // 已归档（compact 旧世代/手动）
	Msgs         int               `json:"msgs"`
	CreatedAt    int64             `json:"createdAt"`
	TargetID     string            `json:"targetId,omitempty"` // 向上边（前端组树用）
	ForkedFrom   *hooks.ForkOrigin `json:"forkedFrom,omitempty"`
	LineRoot     string            `json:"lineRoot,omitempty"`
	IsLeaf       bool              `json:"isLeaf"`       // 所属线的当前叶（可切换进入）
	IsActiveLine bool              `json:"isActiveLine"` // 所属线是当前分支
}

/*
Tree 返回全部 session 节点（扁平，前端按 targetId 组树）。
记忆页 = 完整会话树：归档世代带主题与"已归档"标识，fork 分叉可见。
*/
func (t *TopicService) Tree(ctx context.Context) []SessionNode {
	entries := map[string]hooks.TopicEntry{}
	for _, e := range t.Hub.Topics.Load() {
		entries[e.ID] = e
	}
	active := t.Hub.Active
	ids, _ := hooks.ListMain(ctx, t.Hub.Fsys)
	out := make([]SessionNode, 0, len(ids))
	for _, id := range ids {
		snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, id)
		if err != nil {
			continue
		}
		// session 名称：优先快照 Title（本代首条 user / fork 锚点），
		// 空则 FirstUserTitle 兜底；叶子不借用线标题（身份分离）
		title := snap.Title
		if title == "" {
			title = hooks.FirstUserTitle(snap.Messages)
		}
		n := SessionNode{
			ID: id, Title: title,
			SeedKind: snap.SeedKind, Archived: snap.Archived,
			Msgs: len(snap.Messages), CreatedAt: snap.CreatedAt,
			TargetID: snap.TargetID, ForkedFrom: snap.ForkedFrom, LineRoot: snap.LineRoot,
		}
		if e, ok := entries[snap.LineRoot]; ok {
			if id == e.LeafID {
				n.IsLeaf = true
			}
			if active != nil && e.ID == active.RootID {
				n.IsActiveLine = true
			}
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

/*
Archive 手动归档开关（预留：标记会话不再作为恢复候选，树上显示已归档）。
活动分支的当前叶拒绝——它还会被继续写入，标记会被下次落盘覆盖；
归档一条线的当前叶前先切到别的分支。
*/
func (t *TopicService) Archive(ctx context.Context, id string, archived bool) error {
	snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, id)
	if err != nil {
		return ErrTopicNotFound
	}
	if s := t.Hub.SessionOf(snap.LineRoot); s != nil && s.ID == id && (s == t.Hub.Active || s.Busy()) {
		return ErrCantArchive
	}
	snap.Archived = archived
	return hooks.SaveSnap(ctx, t.Hub.Fsys, snap)
}

/*
Compact 归档换代（用户按键触发，分支列表行操作）：把指定分支的当前
话题总结归档并开新会话——旧库封存、新库 system 注入摘要、话题线换代
（树的纵深）。rootID 空 = 活动分支。与 trim（模型侧上下文整理，就地
不换库）相对。摘要期间持归档锁：锁发消息/切分支/防二次触发。
*/
func (t *TopicService) Compact(ctx context.Context, rootID string) error {
	s := t.Hub.Active
	if rootID != "" {
		if v := t.Hub.SessionOf(rootID); v != nil {
			s = v
		}
	}
	if s == nil {
		return ErrTopicNotFound
	}
	if !s.BeginArchive() { // 原子检查 busy + 防重入
		return domain.ErrBusy
	}
	defer s.EndArchive()
	w := s.Wired()
	sys := s.SysPromptRef()
	if w == nil || w.Provider == nil || sys == nil {
		return errors.New("session not assembled")
	}
	st := t.Hub.SettingsSnapshot()
	info, err := hooks.ArchiveSession(ctx, w.Provider, t.Hub.Fsys, s.Sess, sys,
		s.Topics, w.Trace, func() string { return buildSystemBase(ctx, st, s.Fsys) },
		s.History(), s.ModelView())
	if err != nil {
		return err
	}
	s.RotateTo(info.NewID) // 内存换代：清历史/水位，下一条输入落新库
	s.Publish(domain.Event{Type: "session.compact", Data: domain.Raw(info)})
	return nil
}
