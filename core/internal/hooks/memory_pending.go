/*
memory_pending 是记忆待确认操作层：memory 工具的变更不直接写记忆，
先落待确认区（memory/pending-ops.json，单文件数组，重启不丢）。
用户在通知栏逐条确认（应用）或取消（丢弃）——确认应用复用与工具一致的
写入逻辑；变更随后经 remind 轮首 diff 进 resource_change。

进程内互斥锁串行化全部读写：同一轮并行多个 memory 调用各成一条独立
记录（此前直接写目标文件存在读-改-写竞态，后写覆盖先写丢条目）。
*/
package hooks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xuanlv2002/ezloop/ext/fs"
)

/* PendingOpsPath 是待确认操作存储（memory/pending-ops.json）。 */
const PendingOpsPath = "memory/pending-ops.json"

/* 待确认操作类型与来源。 */
const (
	PendingOpAdd           = "add"
	PendingOpUpdate        = "update"
	PendingOpRemove        = "remove"
	PendingOpProjectSave   = "project-save"
	PendingOpProjectRemove = "project-remove"
)

/* PendingMemoryOp 是一条待确认的记忆操作（确认时才应用到目标）。 */
type PendingMemoryOp struct {
	ID          string    `json:"id"`
	Op          string    `json:"op"`
	Target      string    `json:"target,omitempty"` // user | soul（条目类）
	ItemID      string    `json:"itemId,omitempty"` // update/remove 的条目 id
	Text        string    `json:"text,omitempty"`
	ProjectID   string    `json:"projectId,omitempty"` // project-* 的项目 id
	Name        string    `json:"name,omitempty"`
	Description string    `json:"description,omitempty"`
	Content     string    `json:"content,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

/* pendingMu 串行化待确认区与确认应用的全部读写。 */
var pendingMu sync.Mutex

/* Describe 渲染操作的一句话描述（通知与卡片展示）。 */
func (p PendingMemoryOp) Describe() string {
	switch p.Op {
	case PendingOpAdd:
		return "新增记忆 " + p.Target + "：" + pendingOneLine(p.Text)
	case PendingOpUpdate:
		return "更新记忆 " + p.Target + " " + p.ItemID + "：" + pendingOneLine(p.Text)
	case PendingOpRemove:
		return "删除记忆 " + p.Target + " " + p.ItemID
	case PendingOpProjectSave:
		return "保存项目记忆 " + p.Name + "（" + p.Description + "）"
	case PendingOpProjectRemove:
		return "删除项目记忆 " + p.ProjectID
	}
	return p.Op
}

/* DescribeDetail 渲染通知详情（摘要 + 正文全文，用户确认前查看）。 */
func (p PendingMemoryOp) DescribeDetail() string {
	body := p.Text
	if p.Op == PendingOpProjectSave || p.Op == PendingOpProjectRemove {
		body = p.Content
	}
	if strings.TrimSpace(body) == "" {
		return p.Describe()
	}
	return p.Describe() + "\n" + body
}

/* pendingOneLine 压平换行截摘要。 */
func pendingOneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}

/* LoadPendingOps 读全部待确认操作（缺失=空；时间序）。 */
func LoadPendingOps(ctx context.Context, fsys fs.FileSystem) ([]PendingMemoryOp, error) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	return loadPendingOpsLocked(ctx, fsys)
}

func loadPendingOpsLocked(ctx context.Context, fsys fs.FileSystem) ([]PendingMemoryOp, error) {
	data, err := fsys.Read(ctx, PendingOpsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ops []PendingMemoryOp
	if len(data) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(data, &ops); err != nil {
		return nil, fmt.Errorf("memory pending ops: %w", err)
	}
	return ops, nil
}

/* AppendPendingOp 追加一条待确认操作（生成新 ID），返回该操作。 */
func AppendPendingOp(ctx context.Context, fsys fs.FileSystem, op PendingMemoryOp) (PendingMemoryOp, error) {
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	op.ID = fmt.Sprintf("po%d-%s", time.Now().UnixNano(), hex.EncodeToString(suffix[:]))
	if op.CreatedAt.IsZero() {
		op.CreatedAt = time.Now()
	}
	pendingMu.Lock()
	defer pendingMu.Unlock()
	ops, err := loadPendingOpsLocked(ctx, fsys)
	if err != nil {
		return op, err
	}
	return op, savePendingOpsLocked(ctx, fsys, append(ops, op))
}

/*
	DecidePendingOp 确认（accept=true 应用）或取消（丢弃）一条操作，

返回结果描述（找不到该操作报错）。
*/
func DecidePendingOp(ctx context.Context, fsys fs.FileSystem, id string, accept bool) (string, error) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	ops, err := loadPendingOpsLocked(ctx, fsys)
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(ops, func(o PendingMemoryOp) bool { return o.ID == id })
	if i < 0 {
		return "", os.ErrNotExist
	}
	op := ops[i]
	if !accept {
		return "已取消：" + op.Describe(), savePendingOpsLocked(ctx, fsys, slices.Delete(ops, i, i+1))
	}
	result, err := applyPendingOpLocked(ctx, fsys, op)
	if err != nil {
		return "", err
	}
	return result, savePendingOpsLocked(ctx, fsys, slices.Delete(ops, i, i+1))
}

/* applyPendingOpLocked 应用单条操作到记忆（锁内调用）。 */
func applyPendingOpLocked(ctx context.Context, fsys fs.FileSystem, op PendingMemoryOp) (string, error) {
	switch op.Op {
	case PendingOpAdd:
		path, prefix, err := pendingItemPath(op.Target)
		if err != nil {
			return "", err
		}
		items, err := LoadItems(ctx, fsys, path)
		if err != nil {
			return "", err
		}
		item := MemoryItem{ID: NextItemID(items, prefix), Text: strings.TrimSpace(op.Text), UpdatedAt: time.Now()}
		if err := SaveItems(ctx, fsys, path, append(items, item)); err != nil {
			return "", err
		}
		return "已新增记忆 " + op.Target + " " + item.ID + "：" + pendingOneLine(item.Text), nil
	case PendingOpUpdate:
		path, _, err := pendingItemPath(op.Target)
		if err != nil {
			return "", err
		}
		items, err := LoadItems(ctx, fsys, path)
		if err != nil {
			return "", err
		}
		i := slices.IndexFunc(items, func(it MemoryItem) bool { return it.ID == op.ItemID })
		if i < 0 {
			return "条目 " + op.ItemID + " 已不存在，忽略本次更新", nil
		}
		items[i].Text = strings.TrimSpace(op.Text)
		items[i].UpdatedAt = time.Now()
		if err := SaveItems(ctx, fsys, path, items); err != nil {
			return "", err
		}
		return "已更新记忆 " + op.Target + " " + op.ItemID, nil
	case PendingOpRemove:
		path, _, err := pendingItemPath(op.Target)
		if err != nil {
			return "", err
		}
		items, err := LoadItems(ctx, fsys, path)
		if err != nil {
			return "", err
		}
		i := slices.IndexFunc(items, func(it MemoryItem) bool { return it.ID == op.ItemID })
		if i < 0 {
			return "条目 " + op.ItemID + " 已不存在，忽略本次删除", nil
		}
		if err := SaveItems(ctx, fsys, path, slices.Delete(items, i, i+1)); err != nil {
			return "", err
		}
		return "已删除记忆 " + op.Target + " " + op.ItemID, nil
	case PendingOpProjectSave:
		id := strings.TrimSpace(op.ProjectID)
		if id == "" {
			id = ProjectSlug(op.Name)
		}
		if id == "" {
			return "", errors.New("项目名无法生成有效 id")
		}
		projects, err := LoadProjects(ctx, fsys)
		if err != nil {
			return "", err
		}
		created := true
		if i := slices.IndexFunc(projects, func(p ProjectEntry) bool { return p.ID == id }); i >= 0 {
			created = false
			projects[i].Name = strings.TrimSpace(op.Name)
			projects[i].Description = strings.TrimSpace(op.Description)
		} else {
			projects = append(projects, ProjectEntry{ID: id, Name: strings.TrimSpace(op.Name), Description: strings.TrimSpace(op.Description)})
		}
		if err := SaveProjects(ctx, fsys, projects); err != nil {
			return "", err
		}
		if err := fsys.Write(ctx, ProjectContentPath(id), []byte(strings.TrimSpace(op.Content)+"\n")); err != nil {
			return "", err
		}
		verb := "已更新"
		if created {
			verb = "已新建"
		}
		return verb + "项目记忆 " + id + "（" + op.Name + "）", nil
	case PendingOpProjectRemove:
		projects, err := LoadProjects(ctx, fsys)
		if err != nil {
			return "", err
		}
		i := slices.IndexFunc(projects, func(p ProjectEntry) bool { return p.ID == op.ProjectID })
		if i < 0 {
			return "项目 " + op.ProjectID + " 已不存在，忽略", nil
		}
		if err := SaveProjects(ctx, fsys, slices.Delete(projects, i, i+1)); err != nil {
			return "", err
		}
		if err := os.Remove(filepath.FromSlash(ProjectContentPath(op.ProjectID))); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return "已删除项目记忆 " + op.ProjectID, nil
	}
	return "", fmt.Errorf("未知操作类型 %q", op.Op)
}

/* pendingItemPath 按 target 取条目文件与前缀。 */
func pendingItemPath(target string) (string, string, error) {
	switch target {
	case "user":
		return UserJson, "u", nil
	case "soul":
		return SoulJson, "s", nil
	}
	return "", "", errors.New("target 需为 user 或 soul")
}

/* savePendingOpsLocked 写待确认区（锁内调用；osfs.Write 自动建目录）。 */
func savePendingOpsLocked(ctx context.Context, fsys fs.FileSystem, ops []PendingMemoryOp) error {
	b, err := json.MarshalIndent(ops, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(ctx, PendingOpsPath, b)
}
