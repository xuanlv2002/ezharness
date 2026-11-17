/*
记忆树目录布局与条目式存储（注入走 buildSystemBase 的 <memory> 段）：
user（用户信息）/soul（agent 的灵魂：用户对 agent 的期望）是条目集合
（数据库式，唯一键 ID 一条条），项目记忆 = 索引（projects.json）+ 每项目
一个全文 md。agent 侧读写只能走 memory 工具，用户侧在记忆页结构化编辑
——两端共用本存储层。
*/
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/ext/fs"
)

/* 记忆体系目录布局（工作目录相对）。 */
const (
	MemoryDir    = "memory"
	LongtermDir  = "memory/longterm"
	SkillsDir    = "memory/skills"
	UserJson     = LongtermDir + "/user.json"     // 用户个人信息（条目式）
	SoulJson     = LongtermDir + "/soul.json"     // agent soul（用户对 agent 的期望，条目式）
	ProjectsJson = LongtermDir + "/projects.json" // 项目索引（条目式）
)

/* MemoryItem 是 user/soul 的一条记忆（唯一键 ID，前缀 u/s 区分目标）。 */
type MemoryItem struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	UpdatedAt time.Time `json:"updatedAt"`
}

/* ProjectEntry 是项目索引条目；记忆全文在 project-<id>.md。 */
type ProjectEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

/* LoadItems 读条目文件（缺失/空文件=空集合）。 */
func LoadItems(ctx context.Context, fsys fs.FileSystem, path string) ([]MemoryItem, error) {
	data, err := fsys.Read(ctx, path)
	if err != nil || len(data) == 0 {
		return nil, nil
	}
	var items []MemoryItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("memory items %s: %w", path, err)
	}
	return items, nil
}

/* SaveItems 写条目文件。 */
func SaveItems(ctx context.Context, fsys fs.FileSystem, path string, items []MemoryItem) error {
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(ctx, path, b)
}

/* LoadProjects 读项目索引（缺失=空）。 */
func LoadProjects(ctx context.Context, fsys fs.FileSystem) ([]ProjectEntry, error) {
	data, err := fsys.Read(ctx, ProjectsJson)
	if err != nil || len(data) == 0 {
		return nil, nil
	}
	var projects []ProjectEntry
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, fmt.Errorf("memory projects: %w", err)
	}
	return projects, nil
}

/* SaveProjects 写项目索引。 */
func SaveProjects(ctx context.Context, fsys fs.FileSystem, projects []ProjectEntry) error {
	b, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(ctx, ProjectsJson, b)
}

/* MaxItemNumber 返回同前缀条目的最大序号（0=无）。 */
func MaxItemNumber(items []MemoryItem, prefix string) int {
	max := 0
	for _, it := range items {
		if n, err := strconv.Atoi(strings.TrimPrefix(it.ID, prefix)); err == nil && n > max {
			max = n
		}
	}
	return max
}

/* NextItemID 生成下一条目 id：同前缀现有最大序号 +1（u3/s7）。 */
func NextItemID(items []MemoryItem, prefix string) string {
	return fmt.Sprintf("%s%d", prefix, MaxItemNumber(items, prefix)+1)
}

/* ProjectContentPath 项目记忆全文路径。 */
func ProjectContentPath(id string) string {
	return LongtermDir + "/project-" + id + ".md"
}

/*
ProjectSlug 规范项目标识：小写、空白与下划线转连字符、去文件名非法
字符（保留中英文数字与连字符），压缩连续连字符，截 64 字符。
*/
func ProjectSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-',
			r >= 0x4e00 && r <= 0x9fff:
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '\t':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
