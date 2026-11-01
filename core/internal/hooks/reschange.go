/*
reschange 是 remind hook 的变更段：轮首对比 session 资源基线
（skills/mcps/apps），有变化才产出条目（由 remind.OnStart 汇总为
<resource_change> 消息与 resource.change 事件，并附变更后完整清单）。

基线（ResSnapshot）持久化随 session：sessionstore 落盘、restoreFrom
还原，重启不重复报。首轮只建基线不 diff（迁移/新建会话不算变更）。
技能/MCP/可选模型启停也算变更：基线记录启用/停用两份名单，启停切换
渲染为"启用/停用 <资源> <名>"（与新增/移除一体）；旧格式基线
（V 低于 resSnapV，缺停用名单）视为未建，静默重建，不把存量停用
误报成新增。
*/
package hooks

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/skill"

	"ezharness/core/internal/builtinskill"
	"ezharness/core/internal/callback"
	"ezharness/core/internal/quickapp"
)

/* StatusMcp 是状态基线与 available 清单用的 MCP 服务摘要。 */
type StatusMcp struct {
	Name string
	Desc string
}

/* StatusApp 是快应用基线与 available 清单摘要。 */
type StatusApp struct {
	Name        string
	Title       string
	Version     string
	Description string
	Callable    bool
}

/*
buildChanges 对比资源基线产出变更条目（中文完整短语，模型与前端
变更卡同源）与变更后完整清单（名+描述，技能只列启用的），并推进基线。
prev 为 nil（首轮或旧格式）只建基线。技能/MCP/可选模型均按启用清单 +
停用名单分列，区分「新增/移除」与「启用/停用」。
*/
func (h *Remind) buildChanges(ctx context.Context) (items []string, skills, mcps, models []StatusMcp, apps []StatusApp, callbacks []callback.Task) {
	var off []string
	if h.disabled != nil {
		off = h.disabled()
	}
	isOff := func(path string) bool { return slices.Contains(off, SkillDirOf(path)) }

	// 全量收集（内建优先，与 <skills> 段同一归置），启停只标注不过滤
	type skillState struct {
		Name string
		Desc string
		Off  bool
	}
	var all []skillState
	for _, s := range builtinskill.Skills() {
		all = append(all, skillState{Name: s.Name, Desc: s.Description, Off: isOff(s.Path)})
	}
	if loaded, err := skill.LoadDir(ctx, h.fsys, SkillsDir); err == nil {
		for _, s := range loaded {
			if slices.ContainsFunc(all, func(x skillState) bool { return x.Name == s.Name }) {
				continue
			}
			all = append(all, skillState{Name: s.Name, Desc: s.Description, Off: isOff(s.Path)})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	// 启用/停用分列；available 清单与返回值只含启用的
	var onNames, offNames []string
	for _, s := range all {
		if s.Off {
			offNames = append(offNames, s.Name)
		} else {
			onNames = append(onNames, s.Name)
			skills = append(skills, StatusMcp{Name: s.Name, Desc: s.Desc})
		}
	}

	// MCP/可选模型同构：启用清单 + 停用名单（任一槽可用即算启用）
	var mcpsOff, modelsOff []string
	if h.mcpList != nil {
		mcps = h.mcpList()
		sort.Slice(mcps, func(i, j int) bool { return mcps[i].Name < mcps[j].Name })
		if h.mcpOff != nil {
			mcpsOff = notIn(h.mcpOff(), names(mcps))
			sort.Strings(mcpsOff)
		}
	}
	if h.modelList != nil {
		models = h.modelList()
		sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
		if h.modelOff != nil {
			modelsOff = notIn(h.modelOff(), names(models))
			sort.Strings(modelsOff)
		}
	}

	// 长期记忆基线（memory 工具轮内改 / distill 归档应用，下轮 diff 出变更）
	memSigs := memorySignatures(ctx, h.fsys)
	projSigs := projectSignatures(ctx, h.fsys)

	// 快应用基线（与技能同构：清单进 system，轮内增删改由 resource_change 报告）
	appSigs := make([]string, 0)
	for _, a := range quickapp.Scan(ctx, h.fsys) {
		apps = append(apps, StatusApp{Name: a.Name, Title: a.Title, Version: a.Version,
			Description: a.Description, Callable: a.Callable})
		appSigs = append(appSigs, appSignature(a))
	}

	// 定时回调基线（任务配置与启停；运行态不进签名，避免每次触发都报变更）。
	// 已完成的一次性任务不算资源：不进签名与清单，完成当轮按「移除」报告一次。
	callbackSigs := make([]string, 0)
	for _, t := range callback.LoadTasks(ctx, h.fsys) {
		if t.Finished() {
			continue
		}
		callbacks = append(callbacks, t)
		callbackSigs = append(callbackSigs, t.Signature())
	}

	prev := h.store.ResSnap()
	curModel := ""
	if h.model != nil {
		curModel = h.model()
	}
	if prev != nil && prev.V == resSnapV {
		items = append(items, diffToggleNames(prev.Skills, prev.SkillsOff, onNames, offNames, "技能")...)
		items = append(items, diffToggleNames(prev.Mcps, prev.McpsOff, names(mcps), mcpsOff, "MCP 服务")...)
		items = append(items, diffToggleNames(prev.Models, prev.ModelsOff, names(models), modelsOff, "可选模型")...)
		// 主模型切换同通道：轮首 diff 出变更条目（落盘 + 时间线卡 + 模型可见）
		if prev.Model != "" && curModel != "" && curModel != prev.Model {
			items = append(items, "切换模型到 "+curModel)
		}
		items = append(items, diffMemorySignatures(prev.Memory, memSigs)...)
		items = append(items, diffProjectSignatures(prev.Projects, projSigs)...)
		items = append(items, diffAppSignatures(prev.Apps, appSigs)...)
		items = append(items, diffCallbackSignatures(prev.Callbacks, callbackSigs)...)
	}

	// 推进基线（启用/停用分列）
	h.store.SetResSnap(&ResSnapshot{V: resSnapV, Skills: onNames, SkillsOff: offNames,
		Mcps: names(mcps), McpsOff: mcpsOff, Models: names(models), ModelsOff: modelsOff,
		Model: curModel, Memory: memSigs, Projects: projSigs, Apps: appSigs, Callbacks: callbackSigs})
	return items, skills, mcps, models, apps, callbacks
}

/* appSignature 生成应用基线签名（描述放最后，允许含分隔符）。 */
func appSignature(a quickapp.Info) string {
	callable := "0"
	if a.Callable {
		callable = "1"
	}
	callbacks := make([]string, 0, len(a.Callbacks))
	for _, c := range a.Callbacks {
		callbacks = append(callbacks, c.Name+"="+c.Method+" "+strings.ReplaceAll(c.URL, "|", " "))
	}
	title := strings.ReplaceAll(a.Title, "|", " ")
	return strings.Join([]string{a.Name, title, a.Version, callable, strings.Join(callbacks, ","), a.Description}, "|")
}

/*
diffAppSignatures 对比应用签名：新增/移除按 id，标题/版本/描述/可调用性
变化记为更新。渲染 "新增快应用 id（标题）" 等完整短语。
*/
func diffAppSignatures(oldS, newS []string) []string {
	parts := func(s string) []string { return strings.SplitN(s, "|", 6) }
	label := func(p []string) string {
		if len(p) > 1 && p[1] != "" && p[1] != p[0] {
			return p[0] + "（" + p[1] + "）"
		}
		return p[0]
	}
	var out []string
	for _, s := range newS {
		p := parts(s)
		if i := slices.IndexFunc(oldS, func(o string) bool { return parts(o)[0] == p[0] }); i >= 0 {
			if oldS[i] != s {
				out = append(out, "更新快应用 "+label(p))
			}
			continue
		}
		out = append(out, "新增快应用 "+label(p))
	}
	for _, s := range oldS {
		p := parts(s)
		if !slices.ContainsFunc(newS, func(n string) bool { return parts(n)[0] == p[0] }) {
			out = append(out, "移除快应用 "+label(p))
		}
	}
	return out
}

/*
diffCallbackSignatures 对比定时回调签名：新增/移除按 ID，标题/计划/目标/
内容变化记为更新；仅启停位变化时渲染「启用/停用定时回调」。
*/
func diffCallbackSignatures(oldS, newS []string) []string {
	parts := func(s string) []string { return strings.SplitN(s, "|", 7) }
	label := func(p []string) string {
		if len(p) > 1 && p[1] != "" {
			return p[1]
		}
		return p[0]
	}
	enabledOnly := func(a, b []string) bool {
		if len(a) != 7 || len(b) != 7 {
			return false
		}
		for i := range a {
			if i == 3 {
				continue
			}
			if a[i] != b[i] {
				return false
			}
		}
		return a[3] != b[3]
	}
	var out []string
	for _, s := range newS {
		p := parts(s)
		i := slices.IndexFunc(oldS, func(o string) bool { return parts(o)[0] == p[0] })
		if i < 0 {
			out = append(out, "新增定时回调 "+label(p))
			continue
		}
		old := parts(oldS[i])
		if oldS[i] == s {
			continue
		}
		if enabledOnly(old, p) {
			if p[3] == "1" {
				out = append(out, "启用定时回调 "+label(p))
			} else {
				out = append(out, "停用定时回调 "+label(p))
			}
			continue
		}
		out = append(out, "更新定时回调 "+label(p))
	}
	for _, s := range oldS {
		p := parts(s)
		if !slices.ContainsFunc(newS, func(n string) bool { return parts(n)[0] == p[0] }) {
			out = append(out, "移除定时回调 "+label(p))
		}
	}
	return out
}

/* memorySignatures 收集 user/soul 条目签名（"目标 id|text"，text 压平单行）。 */
func memorySignatures(ctx context.Context, fsys fs.FileSystem) []string {
	var out []string
	for _, t := range []struct {
		name string
		path string
	}{
		{"user", UserJson},
		{"soul", SoulJson},
	} {
		items, err := LoadItems(ctx, fsys, t.path)
		if err != nil {
			continue // 读失败不挡变更检测
		}
		for _, it := range items {
			out = append(out, t.name+" "+it.ID+"|"+strings.ReplaceAll(strings.ReplaceAll(it.Text, "\r", ""), "\n", " "))
		}
	}
	return out
}

/* projectSignatures 收集项目签名（"id|name|description|内容字节数"）——
名称、描述与记忆全文变化都能 diff 出（记忆页手动编辑同样触发）。 */
func projectSignatures(ctx context.Context, fsys fs.FileSystem) []string {
	projects, err := LoadProjects(ctx, fsys)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(projects))
	for _, p := range projects {
		n := -1
		if data, rerr := fsys.Read(ctx, ProjectContentPath(p.ID)); rerr == nil {
			n = len(data)
		}
		out = append(out, fmt.Sprintf("%s|%s|%s|%d", p.ID, p.Name, p.Description, n))
	}
	return out
}

/*
diffMemorySignatures 对比记忆条目签名：签名键 = 目标+id，文本不同 =
更新；新增/移除按键。渲染 "新增记忆 user u8：…"（与 skill/mcp 条目
同格式，前端变更卡与通知按"记忆"前缀识别）。
*/
func diffMemorySignatures(oldS, newS []string) []string {
	key := func(s string) string { return s[:strings.Index(s, "|")] }
	text := func(s string) string { return s[strings.Index(s, "|")+1:] }
	var out []string
	for _, s := range newS {
		k := key(s)
		if i := slices.IndexFunc(oldS, func(o string) bool { return key(o) == k }); i >= 0 {
			if text(oldS[i]) != text(s) {
				out = append(out, "更新记忆 "+k+"："+oneLine80(text(s)))
			}
			continue
		}
		out = append(out, "新增记忆 "+k+"："+oneLine80(text(s)))
	}
	for _, s := range oldS {
		k := key(s)
		if !slices.ContainsFunc(newS, func(n string) bool { return key(n) == k }) {
			out = append(out, "移除记忆 "+k)
		}
	}
	return out
}

/* diffProjectSignatures 对比项目签名（条目带名称与描述）。 */
func diffProjectSignatures(oldS, newS []string) []string {
	parts := func(s string) []string { return strings.SplitN(s, "|", 4) }
	var out []string
	for _, s := range newS {
		p := parts(s)
		if i := slices.IndexFunc(oldS, func(o string) bool { return parts(o)[0] == p[0] }); i >= 0 {
			if oldS[i] != s {
				out = append(out, "更新项目记忆 "+p[1]+"（"+p[2]+"）")
			}
			continue
		}
		out = append(out, "新增项目记忆 "+p[1]+"（"+p[2]+"）")
	}
	for _, s := range oldS {
		p := parts(s)
		if !slices.ContainsFunc(newS, func(n string) bool { return parts(n)[0] == p[0] }) {
			out = append(out, "移除项目记忆 "+p[0])
		}
	}
	return out
}

/* oneLine80 截一行摘要（变更条目回显用）。 */
func oneLine80(s string) string {
	if r := []rune(s); len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}

/* names 取摘要清单的名字序列（基线 diff 用）。 */
func names(list []StatusMcp) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.Name
	}
	return out
}

/* notIn 返回 src 中不在 exclude 里的名字（同一模型可能多槽出现：任一可用即算启用）。 */
func notIn(src, exclude []string) []string {
	var out []string
	for _, s := range src {
		if !slices.Contains(exclude, s) {
			out = append(out, s)
		}
	}
	return out
}

/*
diffToggleNames 对比启用清单 + 停用名单产出变更记录：两个旧名单都没有 =
新增；启用→停用 = 停用；停用→启用 = 启用；旧名单有而新名单全无 = 移除。
*/
func diffToggleNames(oldOn, oldOff, newOn, newOff []string, label string) []string {
	known := append(slices.Clone(oldOn), oldOff...)
	all := append(slices.Clone(newOn), newOff...)
	var out []string
	for _, n := range all {
		switch {
		case !slices.Contains(known, n):
			out = append(out, "新增"+label+" "+n)
		case slices.Contains(oldOn, n) && slices.Contains(newOff, n):
			out = append(out, "停用"+label+" "+n)
		case slices.Contains(oldOff, n) && slices.Contains(newOn, n):
			out = append(out, "启用"+label+" "+n)
		}
	}
	for _, n := range known {
		if !slices.Contains(all, n) {
			out = append(out, "移除"+label+" "+n)
		}
	}
	return out
}
