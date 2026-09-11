/*
reschange 是 remind hook 的变更段：轮首对比 session 资源基线
（skills/mcps/terms），收割用户终端操作，有变化才产出条目（由
remind.OnStart 汇总为 <res_change> 消息与 res.change 事件）。

基线（ResSnapshot）持久化随 session：sessionstore 落盘、restoreFrom
还原，重启不重复报。首轮只建基线不 diff（迁移/新建会话不算变更）；
用户终端操作是事件型（服务侧队列收割即清），不走基线、首轮也报。
*/
package hooks

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/hook/skill"
)

/* StatusMcp 是状态基线用的 MCP 服务摘要。 */
type StatusMcp struct {
	Name string
	Desc string
}

/* StatusTerm 是终端摘要（Origin：用户 / AI / AI·<会话名>）。 */
type StatusTerm struct {
	ID     string
	Name   string
	Exited bool
	Origin string
}

/* UserAction 是用户在终端的手动输入（AI 写入不记录）。 */
type UserAction struct {
	ID   string
	Line string
}

/* TermReport 是 remind 向服务层要的终端状态面。 */
type TermReport struct {
	Terms []StatusTerm
	Lines []UserAction
}

/*
buildChanges 对比资源基线产出变更条目（中文完整短语，模型与前端
变更卡同源），并推进基线。语义保序：prev 为 nil（首轮）只建基线；
用户终端操作在 prev 判断外（收割即清，漏报即丢失）。
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

	/* 终端：清单变更走基线 diff（新增/退出/关闭）；用户手动输入收割即
	注入（服务侧队列取走即清，AI 写入不记录） */
	var curTerms []string
	var userChanges []string
	if h.termRep != nil {
		rep := h.termRep()
		for _, t := range rep.Terms {
			curTerms = append(curTerms, termKey(t))
		}
		for _, a := range rep.Lines {
			userChanges = append(userChanges, fmt.Sprintf("用户在终端 %s 执行：%s", a.ID, a.Line))
		}
	}

	var changes []string
	if prev := h.store.ResSnap(); prev != nil {
		changes = append(diffNames(prev.Skills, curSkills, "skill"),
			diffNames(prev.Mcps, curMcps, "mcp")...)
		changes = append(changes, diffTerms(prev.Terms, curTerms)...)
	}
	changes = append(changes, userChanges...)
	h.store.SetResSnap(&ResSnapshot{Skills: curSkills, Mcps: curMcps, Terms: curTerms})
	return changes
}

/* termKey 终端基线编码（id|名称|是否退出|来源）。 */
func termKey(t StatusTerm) string {
	return fmt.Sprintf("%s|%s|%v|%s", t.ID, t.Name, t.Exited, t.Origin)
}

/*
	diffTerms 对比终端基线产出变更：新增（报创建来源——用户手动开的

终端对模型是未知状态，须显式区分）、已退出（退出态翻转）、已修改
（名称/来源变化）、已关闭（消失，AI 可感知用户关掉了自己开的终端）。
*/
func diffTerms(oldS, newS []string) []string {
	parse := func(s string) (id, name, origin string, exited bool) {
		parts := strings.SplitN(s, "|", 4)
		if len(parts) < 3 {
			return s, s, "", false
		}
		exited = parts[2] == "true"
		if len(parts) == 4 {
			return parts[0], parts[1], parts[3], exited
		}
		return parts[0], parts[1], "", exited // 旧 3 段基线（无来源）
	}
	originLabel := func(origin string) string {
		switch {
		case origin == "用户":
			return "，用户手动创建"
		case strings.HasPrefix(origin, "AI"):
			return "，AI 创建"
		}
		return ""
	}
	prev := map[string]string{}
	for _, s := range oldS {
		id, _, _, _ := parse(s)
		prev[id] = s
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range newS {
		id, name, origin, exited := parse(s)
		seen[id] = true
		old, had := prev[id]
		if !had {
			out = append(out, fmt.Sprintf("新增终端 %s(%s)%s", id, name, originLabel(origin)))
			continue
		}
		_, oldName, oldOrigin, wasExited := parse(old)
		if exited && !wasExited {
			out = append(out, fmt.Sprintf("终端 %s(%s) 已退出", id, name))
		}
		if name != oldName || origin != oldOrigin {
			var fields []string
			if name != oldName {
				fields = append(fields, fmt.Sprintf("名称 %q→%q", oldName, name))
			}
			if origin != oldOrigin {
				fields = append(fields, fmt.Sprintf("来源 %q→%q", oldOrigin, origin))
			}
			out = append(out, fmt.Sprintf("终端 %s 已修改（%s）", id, strings.Join(fields, "，")))
		}
	}
	for _, s := range oldS {
		id, name, _, _ := parse(s)
		if !seen[id] {
			out = append(out, fmt.Sprintf("终端 %s(%s) 已关闭", id, name))
		}
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
