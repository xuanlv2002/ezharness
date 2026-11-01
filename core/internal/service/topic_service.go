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
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

/* 按会话 ID 定位时的失败分类（回调任务直连某个叶 session）。 */
var (
	ErrSessionNotFound   = errors.New("session not found")
	ErrSessionArchived   = errors.New("session archived")
	ErrSessionArchiving  = errors.New("session archiving")
	ErrSessionSuperseded = errors.New("session superseded")
)

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
	active := h.ActiveSession()
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
	Entry        hooks.TopicEntry `json:"entry"`
	Messages     []types.Message  `json:"messages"`
	Summary      string           `json:"summary,omitempty"`      // compact 摘要段（压缩新叶几乎无消息，回顾靠它给上下文）
	SystemPrompt string           `json:"systemPrompt,omitempty"` // 落盘时渲染后的完整 system（回顾查看）
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
			return TopicDetail{
				Entry:        entry,
				Messages:     snap.Messages,
				Summary:      snap.SummaryBlock,
				SystemPrompt: snap.SystemPrompt,
			}, nil
		}
	}
	if snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, id); err == nil {
		return TopicDetail{
			Entry:        hooks.TopicEntry{ID: id, Title: hooks.FirstUserTitle(snap.Messages), Msgs: len(snap.Messages)},
			Messages:     snap.Messages,
			Summary:      snap.SummaryBlock,
			SystemPrompt: snap.SystemPrompt,
		}, nil
	}
	return TopicDetail{}, ErrTopicNotFound
}

/*
Trace 读取分支的调用链 span（与 Get 同一叶解析：索引命中取 LeafID，
否则 id 自身；无 trace.jsonl 返回空切片）。
*/
func (t *TopicService) Trace(ctx context.Context, id string) []hooks.Span {
	if entry, ok := t.Hub.Topics.Get(id); ok && entry.LeafID != "" {
		id = entry.LeafID
	}
	if spans := hooks.LoadTrace(ctx, t.Hub.Fsys, id); spans != nil {
		return spans
	}
	return []hooks.Span{} // 契约是数组：无 trace.jsonl 也要给空数组而非 null
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
	srcSnap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, sourceID)
	if err != nil {
		return nil, ErrTopicNotFound
	}
	src := hooks.RepairForkSnap(ctx, t.Hub.Fsys, srcSnap) // 源是空壳 fork 时先补齐再复制
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
	// 前缀副本写进 fork 自己的 trace.jsonl：session.json 不落消息，
	// 重启后 LoadSnap 靠 trace 合成历史，缺这步则 fork 变空壳
	if err := hooks.AppendMessages(ctx, t.Hub.Fsys, newID, "", snap.Messages); err != nil {
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
	snap = hooks.RepairForkSnap(ctx, t.Hub.Fsys, snap) // 旧版空壳 fork 就地补前缀
	s := t.Hub.LoadBranch(rootID, snap)
	t.Agents.Assemble(s, t.Hub.SettingsSnapshot())
	t.Hub.SetActive(s)
	return nil
}

/*
EnsureSession 返回指定叶 session 的已装配会话（按 session ID 精确定位，
不切换活动分支）。会话已归档、正在归档、已被 compact 换代或还没落盘
时返回对应错误——定时回调绑定的是创建任务时的那个会话，不跟随新叶。
*/
func (t *TopicService) EnsureSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrSessionNotFound
	}
	// 已加载的会话以内存为准：compact 换代时 ID 会随 RotateTo 更新。
	for _, s := range t.Hub.Sessions() {
		if s.CurrentID() != sessionID {
			continue
		}
		if s.Archiving() {
			return nil, ErrSessionArchiving
		}
		if snap, err := hooks.LoadSnapMeta(ctx, t.Hub.Fsys, sessionID); err == nil && snap.Archived {
			return nil, ErrSessionArchived
		}
		return s, nil
	}

	snapMeta, err := hooks.LoadSnapMeta(ctx, t.Hub.Fsys, sessionID)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	if snapMeta.Archived {
		return nil, ErrSessionArchived
	}
	lineRoot := snapMeta.LineRoot
	if lineRoot == "" {
		lineRoot = sessionID
	}
	entry, ok := t.Hub.Topics.Get(lineRoot)
	if !ok {
		return nil, ErrSessionSuperseded
	}
	leaf := entry.LeafID
	if leaf == "" {
		leaf = entry.ID
	}
	if leaf != sessionID {
		return nil, ErrSessionSuperseded
	}
	if s := t.Hub.SessionOf(lineRoot); s != nil {
		if s.CurrentID() != sessionID {
			return nil, ErrSessionSuperseded
		}
		if s.Archiving() {
			return nil, ErrSessionArchiving
		}
		return s, nil
	}
	snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, sessionID)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	if snap.Archived {
		return nil, ErrSessionArchived
	}
	snap = hooks.RepairForkSnap(ctx, t.Hub.Fsys, snap) // 旧版空壳 fork 就地补前缀
	s := t.Hub.LoadBranch(lineRoot, snap)
	t.Agents.Assemble(s, t.Hub.SettingsSnapshot())
	return s, nil
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
	active := t.Hub.ActiveSession()
	if active != nil && active.RootID == rootID {
		t.NewBranch() // 活动分支被删：先切到全新空线
	}
	t.Hub.Unregister(rootID)
	ids, _ := hooks.ListMain(ctx, t.Hub.Fsys)
	for _, id := range ids {
		if id == rootID {
			continue
		}
		if snap, err := hooks.LoadSnapMeta(ctx, t.Hub.Fsys, id); err == nil && snap.LineRoot == rootID {
			if rmErr := os.RemoveAll(filepath.Join(hooks.SessionsDir, id)); rmErr != nil {
				log.Printf("删除会话目录失败（%s）: %v", id, rmErr)
			}
		}
	}
	if rmErr := os.RemoveAll(filepath.Join(hooks.SessionsDir, rootID)); rmErr != nil {
		log.Printf("删除会话目录失败（%s）: %v", rootID, rmErr)
	}
	t.Hub.Topics.Remove(rootID)
	return nil
}

/* SessionNode 是会话树节点（记忆页按层渲染：全部世代与分叉）。 */
type SessionNode struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	SeedKind     string            `json:"seedKind,omitempty"` // new | fork | compress
	Archived     bool              `json:"archived"`           // 已归档（compact 旧世代/手动）
	Msgs         int               `json:"msgs"`
	CreatedAt    int64             `json:"createdAt"`
	TargetID     string            `json:"targetId,omitempty"` // 向上边（压缩链游标）
	ParentID     string            `json:"parentId,omitempty"` // 展示父节点（空 = 顶层分支）
	ForkedFrom   *hooks.ForkOrigin `json:"forkedFrom,omitempty"`
	LineRoot     string            `json:"lineRoot,omitempty"`
	IsLeaf       bool              `json:"isLeaf"`       // 所属线的当前叶（可切换进入）
	IsActiveLine bool              `json:"isActiveLine"` // 所属线是当前分支
	ChildCount   int               `json:"childCount"`   // 直接子代数（0 = 无展开箭头）
}

/* TreePage 是会话树的一层；根层按分支分批，子层一次给全。 */
type TreePage struct {
	Nodes []SessionNode `json:"nodes"`
	Next  string        `json:"next,omitempty"`  // 根层下一页游标（空 = 已到底）
	Total int           `json:"total,omitempty"` // 根层分支总数
}

const (
	treeRootLimit = 30
	treeRootMax   = 200
)

/*
TreeLevel 返回会话树的直接一层（扁平，前端按 parentId 组树）。
parent 为空 = 顶层分支（按线最近活动时间倒序，每页 treeRootLimit 条）；
否则返回该节点的直接子代。每个节点带 childCount，前端据此决定是否
显示展开箭头，并在展开时再拉下一层。

记忆页 = 完整会话树：归档世代带主题与"已归档"标识，fork 分叉可见。
只读 session.json 元数据，不解析 trace 消息档案；空标题/旧快照缺计数
的兜底读取只发生在当前页节点上。
*/
func (t *TopicService) TreeLevel(ctx context.Context, parent, before string, limit int) TreePage {
	entries := map[string]hooks.TopicEntry{}
	for _, e := range t.Hub.Topics.Load() {
		entries[e.ID] = e
	}
	nodes := t.treeNodes(ctx, entries)

	byID := make(map[string]int, len(nodes))
	for i := range nodes {
		byID[nodes[i].ID] = i
	}
	/* 父子关系与前端组树规则一致：compress 挂旧代，fork 挂源旧代的
	   父级（兄弟位）；父级缺失的按根处理。 */
	children := make(map[int][]int, len(nodes))
	var roots []int
	for i := range nodes {
		n := &nodes[i]
		p := -1
		switch {
		case n.SeedKind == "compress" && n.TargetID != "":
			if j, ok := byID[n.TargetID]; ok {
				p = j
			}
		case n.SeedKind == "fork" && n.TargetID != "":
			if src, ok := byID[n.TargetID]; ok && nodes[src].SeedKind == "compress" && nodes[src].TargetID != "" {
				if j, ok := byID[nodes[src].TargetID]; ok {
					p = j
				}
			}
		}
		if p >= 0 {
			n.ParentID = nodes[p].ID
			children[p] = append(children[p], i)
		} else {
			roots = append(roots, i)
		}
	}

	if parent != "" {
		i, ok := byID[parent]
		if !ok {
			return TreePage{Nodes: []SessionNode{}}
		}
		pick := append([]int(nil), children[i]...)
		sort.SliceStable(pick, func(a, b int) bool {
			if nodes[pick[a]].CreatedAt != nodes[pick[b]].CreatedAt {
				return nodes[pick[a]].CreatedAt > nodes[pick[b]].CreatedAt
			}
			return nodes[pick[a]].ID < nodes[pick[b]].ID
		})
		return TreePage{Nodes: t.fillTreeNodes(ctx, nodes, pick, children)}
	}

	// 根层：活跃线在前，再按创建时间、ID 定序（游标与排序共用同一元组）
	sort.SliceStable(roots, func(a, b int) bool {
		ua, ub := treeUpdatedAt(&nodes[roots[a]], entries), treeUpdatedAt(&nodes[roots[b]], entries)
		if ua != ub {
			return ua > ub
		}
		if nodes[roots[a]].CreatedAt != nodes[roots[b]].CreatedAt {
			return nodes[roots[a]].CreatedAt > nodes[roots[b]].CreatedAt
		}
		return nodes[roots[a]].ID < nodes[roots[b]].ID
	})
	start := 0
	if before != "" {
		cu, cc, cid, ok := parseTreeCursor(before)
		if ok {
			for start < len(roots) && !treeRootAfter(&nodes[roots[start]], entries, cu, cc, cid) {
				start++
			}
		}
	}
	if limit <= 0 {
		limit = treeRootLimit
	}
	if limit > treeRootMax {
		limit = treeRootMax
	}
	end := start + limit
	if end > len(roots) {
		end = len(roots)
	}
	page := TreePage{Nodes: t.fillTreeNodes(ctx, nodes, roots[start:end], children), Total: len(roots)}
	if end < len(roots) {
		last := &nodes[roots[end-1]]
		page.Next = formatTreeCursor(treeUpdatedAt(last, entries), last.CreatedAt, last.ID)
	}
	return page
}

/* treeNodes 读取全部会话元数据（不解析 trace 消息档案）。 */
func (t *TopicService) treeNodes(ctx context.Context, entries map[string]hooks.TopicEntry) []SessionNode {
	active := t.Hub.ActiveSession()
	ids, _ := hooks.ListMain(ctx, t.Hub.Fsys)
	out := make([]SessionNode, 0, len(ids))
	for _, id := range ids {
		// 只读元数据：树不需要消息正文（大档案全量解码曾让记忆页随会话数线性变慢）
		snap, err := hooks.LoadSnapMeta(ctx, t.Hub.Fsys, id)
		if err != nil {
			continue
		}
		n := SessionNode{
			ID: id, Title: snap.Title,
			SeedKind: snap.SeedKind, Archived: snap.Archived,
			Msgs: snap.MsgCount, CreatedAt: snap.CreatedAt,
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
	return out
}

/* fillTreeNodes 补展示字段；只对当前页节点做旧快照的档案兜底读取。 */
func (t *TopicService) fillTreeNodes(ctx context.Context, nodes []SessionNode, pick []int, children map[int][]int) []SessionNode {
	items := make([]SessionNode, 0, len(pick))
	for _, i := range pick {
		n := nodes[i]
		n.ChildCount = len(children[i])
		// 空标题才读消息兜底（只发生在真正返回的节点上）
		var msgs []types.Message
		if n.Title == "" {
			msgs = hooks.LoadMessages(ctx, t.Hub.Fsys, n.ID, "")
			n.Title = hooks.FirstUserTitle(msgs)
		}
		if n.Msgs == 0 {
			// 旧快照无计数：只数行前缀兜底（不解析消息体）
			if msgs != nil {
				n.Msgs = len(msgs)
			} else {
				n.Msgs = hooks.CountMessages(ctx, t.Hub.Fsys, n.ID)
			}
		}
		items = append(items, n)
	}
	return items
}

/* treeUpdatedAt 是根层排序/游标用的线最近活动时间（索引缺失回退创建时间）。 */
func treeUpdatedAt(n *SessionNode, entries map[string]hooks.TopicEntry) int64 {
	line := n.LineRoot
	if line == "" {
		line = n.ID
	}
	if e, ok := entries[line]; ok && e.UpdatedAt > 0 {
		return e.UpdatedAt
	}
	return n.CreatedAt
}

/* formatTreeCursor / parseTreeCursor：根层游标 = 排序元组（updatedAt|createdAt|id）。 */
func formatTreeCursor(updated, created int64, id string) string {
	return strconv.FormatInt(updated, 10) + "|" + strconv.FormatInt(created, 10) + "|" + id
}

func parseTreeCursor(s string) (int64, int64, string, bool) {
	parts := strings.SplitN(s, "|", 3)
	if len(parts) != 3 || parts[2] == "" {
		return 0, 0, "", false
	}
	updated, err1 := strconv.ParseInt(parts[0], 10, 64)
	created, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, "", false
	}
	return updated, created, parts[2], true
}

/* treeRootAfter 判断节点是否严格排在游标之后（与根层排序同序）。 */
func treeRootAfter(n *SessionNode, entries map[string]hooks.TopicEntry, updated, created int64, id string) bool {
	u := treeUpdatedAt(n, entries)
	if u != updated {
		return u < updated
	}
	if n.CreatedAt != created {
		return n.CreatedAt < created
	}
	return n.ID > id
}

/*
Archive 手动归档开关（预留：标记会话不再作为恢复候选，树上显示已归档）。
活动分支的当前叶拒绝——它还会被继续写入，标记会被下次落盘覆盖；
归档一条线的当前叶前先切到别的分支。
*/
func (t *TopicService) Archive(ctx context.Context, id string, archived bool) error {
	snap, err := hooks.LoadSnapMeta(ctx, t.Hub.Fsys, id)
	if err != nil {
		return ErrTopicNotFound
	}
	if s := t.Hub.SessionOf(snap.LineRoot); s != nil && s.ID == id && (s == t.Hub.ActiveSession() || s.Busy()) {
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
	s := t.Hub.ActiveSession()
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
		s.Topics, w.Trace, func(nextID string) string {
			return buildSystemBase(ctx, st, s.Fsys, t.Hub.ModelsSnapshot(), nextID)
		},
		s.History(), s.ModelView(), ResolveWorkDir(st.WorkDir))
	if err != nil {
		return err
	}
	s.RotateTo(info.NewID) // 内存换代：清历史/水位，下一条输入落新库
	s.Publish(domain.Event{Type: "session.compact", Data: domain.Raw(info)})
	return nil
}
