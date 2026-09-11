/*
topics 是分支（线）索引：topics.json。条目身份 = 线根 session ID，
LeafID 指向当前叶子（compact 换代只更新 LeafID，不新增条目）。
Kind 记录线的起源（new | fork），Origin 是 fork 线的展示元数据。
*/
package hooks

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/xuanlv2002/ezloop/ext/fs"
)

/* TopicEntry 是一条分支（线）的索引项。 */
type TopicEntry struct {
	ID        string      `json:"id"`               // 线根 session ID
	LeafID    string      `json:"leafId,omitempty"` // 当前叶子（compact 时更新）
	Title     string      `json:"title"`
	Summary   string      `json:"summary,omitempty"`
	CreatedAt int64       `json:"createdAt"`
	UpdatedAt int64       `json:"updatedAt,omitempty"` // 线内最新活动时间
	Msgs      int         `json:"msgs"`
	Kind      string      `json:"kind,omitempty"`   // new | fork
	Origin    *ForkOrigin `json:"origin,omitempty"` // fork 线的分叉源（展示）
}

/* Topics 管理分支索引（topics.json，工作目录）。 */
type Topics struct {
	fsys fs.FileSystem
	mu   sync.Mutex
}

/* NewTopics 创建索引管理器。 */
func NewTopics(fsys fs.FileSystem) *Topics { return &Topics{fsys: fsys} }

/* Load 读取全部索引（文件缺失返回空）。 */
func (t *Topics) Load() []TopicEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.loadLocked()
}

func (t *Topics) loadLocked() []TopicEntry {
	data, err := t.fsys.Read(context.Background(), "topics.json")
	if err != nil {
		return nil
	}
	var raw []TopicEntry
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	// 同 ID 去重（保留最后一条）：历史 bug 可能写入重复条目，读侧自愈
	out := make([]TopicEntry, 0, len(raw))
	idx := map[string]int{}
	for _, e := range raw {
		if i, ok := idx[e.ID]; ok {
			out[i] = e
		} else {
			idx[e.ID] = len(out)
			out = append(out, e)
		}
	}
	return out
}

func (t *Topics) saveLocked(list []TopicEntry) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return t.fsys.Write(context.Background(), "topics.json", data)
}

/* Add 追加（或覆盖）一条分支索引并落盘。 */
func (t *Topics) Add(e TopicEntry) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.loadLocked()
	replaced := false
	for i := range list {
		if list[i].ID == e.ID {
			// 覆盖时保留已有的 Origin/Kind（UpdateLeaf 兜底创建时不带）
			if e.Origin == nil {
				e.Origin = list[i].Origin
			}
			if e.Kind == "" {
				e.Kind = list[i].Kind
			}
			if e.CreatedAt == 0 {
				e.CreatedAt = list[i].CreatedAt
			}
			list[i] = e
			replaced = true
			break
		}
	}
	if !replaced {
		list = append(list, e)
	}
	return t.saveLocked(list)
}

/*
UpdateLeaf 更新线的叶子与最新活动（compact 换代/轮末刷新时调用）。
title 只在兜底创建时使用（线尚未索引：首轮即触发压缩），已有线标题
不覆盖——线标题=首条新增 user，由 SetTitle 显式切换。
*/
func (t *Topics) UpdateLeaf(rootID, leafID, title string, updatedAt int64, msgs int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.loadLocked()
	for i := range list {
		if list[i].ID == rootID {
			list[i].LeafID = leafID
			list[i].UpdatedAt = updatedAt
			list[i].Msgs = msgs
			return t.saveLocked(list)
		}
	}
	if title == "" {
		title = "新对话"
	}
	entry := TopicEntry{
		ID: rootID, LeafID: leafID, Title: title, Kind: "new",
		CreatedAt: updatedAt, UpdatedAt: updatedAt, Msgs: msgs,
	}
	list = append(list, entry)
	return t.saveLocked(list)
}

/* SetTitle 更新线标题（fork 线出现自己的首条新增 user 消息时切换）。 */
func (t *Topics) SetTitle(id, title string) {
	if title == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.loadLocked()
	for i := range list {
		if list[i].ID == id {
			list[i].Title = title
			_ = t.saveLocked(list)
			return
		}
	}
}

/* Remove 删除一条索引并落盘，返回是否存在。 */
func (t *Topics) Remove(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.loadLocked()
	out := list[:0]
	found := false
	for _, e := range list {
		if e.ID == id {
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		return false
	}
	return t.saveLocked(out) == nil
}

/* Get 返回指定线索引（不存在 ok=false）。 */
func (t *Topics) Get(id string) (TopicEntry, bool) {
	for _, e := range t.Load() {
		if e.ID == id {
			return e, true
		}
	}
	return TopicEntry{}, false
}

/*
MigrateIndex 一次性迁移：旧扁平索引（每 compact 世代一条）或缺失索引
重建为分支索引，并回填各快照的 LineRoot。已是新格式则直接返回。
幂等：重复调用无副作用。
*/
func (t *Topics) MigrateIndex(ctx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	legacy := t.loadLocked()
	needs := false
	for _, e := range legacy {
		if e.LeafID == "" || e.Kind == "" || e.Kind == "compact" {
			needs = true
			break
		}
	}
	ids, _ := ListMain(ctx, t.fsys)
	if len(legacy) == 0 && len(ids) > 0 {
		needs = true // 索引缺失但磁盘有会话
	}
	if !needs {
		return
	}

	snaps := map[string]*SessionSnap{}
	for _, id := range ids {
		if s, err := LoadSnap(ctx, t.fsys, id); err == nil {
			snaps[id] = s
		}
	}
	old := map[string]TopicEntry{}
	for _, e := range legacy {
		old[e.ID] = e
	}

	// 上溯找根：沿 compress 边走到空根/new/fork（fork 自成新线根）
	var rootOf func(string, map[string]bool) string
	rootOf = func(id string, seen map[string]bool) string {
		if seen[id] {
			return id // 环防护（理论不可达）
		}
		seen[id] = true
		s := snaps[id]
		if s == nil || s.TargetID == "" || s.SeedKind == "fork" || s.SeedKind == "new" {
			return id
		}
		if _, ok := snaps[s.TargetID]; !ok {
			return id // 上级已被删：自成根
		}
		return rootOf(s.TargetID, seen)
	}

	groups := map[string][]string{}
	for id := range snaps {
		r := rootOf(id, map[string]bool{})
		groups[r] = append(groups[r], id)
	}

	out := make([]TopicEntry, 0, len(groups))
	for root, members := range groups {
		leaf := root
		for _, m := range members {
			if snaps[m].CreatedAt > snaps[leaf].CreatedAt ||
				(snaps[m].CreatedAt == snaps[leaf].CreatedAt && m > leaf) {
				leaf = m
			}
		}
		rs := snaps[root]
		ls := snaps[leaf]
		if len(ls.Messages) == 0 && leaf == root {
			continue // 从未使用的空会话不入索引
		}
		entry := TopicEntry{
			ID:        root,
			LeafID:    leaf,
			CreatedAt: rs.CreatedAt,
			UpdatedAt: ls.CreatedAt,
			Msgs:      len(ls.Messages),
			Kind:      "new",
		}
		if rs.SeedKind == "fork" {
			entry.Kind = "fork"
			srcTitle := FirstUserTitle(ls.Messages)
			if src := snaps[rs.TargetID]; src != nil {
				srcTitle = FirstUserTitle(src.Messages)
			}
			if oe, ok := old[rs.TargetID]; ok && oe.Title != "" {
				srcTitle = oe.Title
			}
			entry.Origin = &ForkOrigin{SourceID: rs.TargetID, Title: srcTitle, Anchor: rs.Anchor}
			if oe, ok := old[root]; ok && oe.Title != "" {
				entry.Title = oe.Title
			}
		}
		if entry.Title == "" {
			if oe, ok := old[root]; ok && oe.Title != "" {
				entry.Title = oe.Title
			} else if oe, ok := old[leaf]; ok && oe.Title != "" {
				entry.Title = oe.Title
			} else {
				entry.Title = FirstUserTitle(ls.Messages)
			}
		}
		if oe, ok := old[leaf]; ok && oe.Summary != "" {
			entry.Summary = oe.Summary
		} else if oe, ok := old[root]; ok && oe.Summary != "" {
			entry.Summary = oe.Summary
		}
		out = append(out, entry)

		// 回填 LineRoot（幂等：已有值跳过）
		for _, m := range members {
			if snaps[m].LineRoot == "" {
				sc := snaps[m]
				sc.LineRoot = root
				_ = SaveSnap(ctx, t.fsys, sc)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	_ = t.saveLocked(out)
}

/* Match 按 id 前缀或标题/摘要包含过滤。q 为空返回全部。 */
func (t *Topics) Match(q string) []TopicEntry {
	list := t.Load()
	if q == "" {
		return list
	}
	var out []TopicEntry
	for _, e := range list {
		if strings.HasPrefix(e.ID, q) ||
			strings.Contains(e.Title, q) || strings.Contains(e.Summary, q) {
			out = append(out, e)
		}
	}
	return out
}
