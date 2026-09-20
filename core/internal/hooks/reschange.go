/*
reschange 是 remind hook 的变更段：轮首对比 session 资源基线
（skills/mcps），有变化才产出条目（由 remind.OnStart 汇总为
<resource_change> 消息与 resource.change 事件，并附变更后完整清单）。

基线（ResSnapshot）持久化随 session：sessionstore 落盘、restoreFrom
还原，重启不重复报。首轮只建基线不 diff（迁移/新建会话不算变更）。
*/
package hooks

import (
	"context"
	"slices"
	"sort"

	"github.com/xuanlv2002/ezloop/ext/hook/skill"
)

/* StatusMcp 是状态基线与 available 清单用的 MCP 服务摘要。 */
type StatusMcp struct {
	Name string
	Desc string
}

/*
buildChanges 对比资源基线产出变更条目（中文完整短语，模型与前端
变更卡同源）与变更后完整清单（名+描述），并推进基线。prev 为 nil
（首轮）只建基线。
*/
func (h *Remind) buildChanges(ctx context.Context) (items []string, skills, mcps []StatusMcp) {
	var off []string
	if h.disabled != nil {
		off = h.disabled()
	}
	if loaded, err := skill.LoadDir(ctx, h.fsys, SkillsDir); err == nil {
		for _, s := range loaded {
			if slices.Contains(off, SkillDirOf(s.Path)) {
				continue
			}
			skills = append(skills, StatusMcp{Name: s.Name, Desc: s.Description})
		}
	}
	mcps = append(mcps, h.mcpList()...)
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	sort.Slice(mcps, func(i, j int) bool { return mcps[i].Name < mcps[j].Name })

	if prev := h.store.ResSnap(); prev != nil {
		items = append(diffNames(prev.Skills, names(skills), "skill"),
			diffNames(prev.Mcps, names(mcps), "mcp")...)
	}
	h.store.SetResSnap(&ResSnapshot{Skills: names(skills), Mcps: names(mcps)})
	return items, skills, mcps
}

/* names 取摘要清单的名字序列（基线 diff 用）。 */
func names(list []StatusMcp) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.Name
	}
	return out
}

/* diffNames 对比新旧名单产出变更记录（中文完整短语，模型可读）。 */
func diffNames(oldS, newS []string, kind string) []string {
	label := map[string]string{"skill": "技能", "mcp": "MCP 服务"}[kind]
	var out []string
	for _, n := range newS {
		if !slices.Contains(oldS, n) {
			out = append(out, "新增"+label+" "+n)
		}
	}
	for _, n := range oldS {
		if !slices.Contains(newS, n) {
			out = append(out, "移除"+label+" "+n)
		}
	}
	return out
}
