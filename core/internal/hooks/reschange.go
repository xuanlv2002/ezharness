/*
reschange 是 remind hook 的变更段：轮首对比 session 资源基线
（skills/mcps/terms），有变化才产出条目（由 remind.OnStart 汇总为
<res_change> 消息与 res.change 事件）。

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

/* StatusMcp 是状态基线用的 MCP 服务摘要。 */
type StatusMcp struct {
	Name string
	Desc string
}

/*
buildChanges 对比资源基线产出变更条目（中文完整短语，模型与前端
变更卡同源），并推进基线。prev 为 nil（首轮）只建基线。
*/
func (h *Remind) buildChanges(ctx context.Context) []string {
	var curSkills, curMcps []string
	var off []string
	if h.disabled != nil {
		off = h.disabled()
	}
	if skills, err := skill.LoadDir(ctx, h.fsys, SkillsDir); err == nil {
		for _, s := range skills {
			if slices.Contains(off, SkillDirOf(s.Path)) {
				continue
			}
			curSkills = append(curSkills, s.Name)
		}
		sort.Strings(curSkills)
	}
	for _, m := range h.mcpList() { // 全量仅作变更基线，不进状态记录
		curMcps = append(curMcps, m.Name)
	}
	sort.Strings(curMcps)

	var changes []string
	if prev := h.store.ResSnap(); prev != nil {
		changes = append(diffNames(prev.Skills, curSkills, "skill"),
			diffNames(prev.Mcps, curMcps, "mcp")...)
	}
	h.store.SetResSnap(&ResSnapshot{Skills: curSkills, Mcps: curMcps})
	return changes
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
