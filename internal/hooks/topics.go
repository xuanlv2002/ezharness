/*
Package xhook 是 ezharness 的策略层 hook 扩展（ezloop 引擎不动）：
话题轮换 rotate、话题回顾 recall、长期记忆 memory 注入。
*/
package hooks

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/xuanlv2002/ezloop/ext/fs"
)

/* TopicEntry 是一条已归档话题的索引项。 */
type TopicEntry struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	CreatedAt int64  `json:"createdAt"`
	Msgs      int    `json:"msgs"`
}

/* Topics 管理归档话题索引（topics.json，工作目录）。 */
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
	var out []TopicEntry
	if json.Unmarshal(data, &out) != nil {
		return nil
	}
	return out
}

/* Add 追加一条索引并落盘。 */
func (t *Topics) Add(e TopicEntry) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.loadLocked()
	list = append(list, e)
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return t.fsys.Write(context.Background(), "topics.json", data)
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
