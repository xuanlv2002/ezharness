/*
status 是 agent 状态栏 hook：每轮 chat 在用户输入前插入一条
<agent_status> user 记录。systemPrompt 每 session 固定，轮内发生的
资源变更（页面新增 mcp、agent 自建 skill）不进 system——模型靠这条
记录获知当前水位与增删变更，直到下个 session 才并入 system。
同时发 status.snapshot 事件供前端渲染状态卡；变更对比靠 Store 里的
资源基线快照（持久化，重启可续），不依赖历史里的旧 status 记录。
*/
package hooks

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/skill"
	"github.com/xuanlv2002/ezloop/types"
)

/* StatusTag 是状态记录的包裹标签。 */
const StatusTag = "agent_status"

/* EventStatus 是状态快照事件（Data 为 StatusData）。 */
const EventStatus = event.EventType("status.snapshot")

/* StatusMcp 是状态栏里的单个 MCP 条目（描述前 8 字）。 */
type StatusMcp struct {
	Name string `json:"name"`
	Desc string `json:"desc,omitempty"`
}

/* StatusData 是状态记录内容（JSON，前端直接解析渲染）。
skill/mcp 全量清单在 system（<skills>/<mcp> 块），这里只注入变更。 */
type StatusData struct {
	Now                string   `json:"now"`
	SinceLastOutputMin int64    `json:"sinceLastOutputMin"` // 0 = 无记录
	CtxTokens          int      `json:"ctxTokens"`
	CtxWindow          int      `json:"ctxWindow"`
	SuggestCompact     bool     `json:"suggestCompact"`
	Changes            []string `json:"changes,omitempty"`
}

/* Status 实现状态栏注入。 */
type Status struct {
	fsys      fs.FileSystem
	store     *Store
	ctxTokens func() int
	ctxWindow int
	mcpList   func() []StatusMcp
}

/*
NewStatus 创建状态栏 hook。ctxTokens 返回最近一次模型调用的 prompt
tokens；ctxWindow 是主模型上下文窗口（<=0 由调用方兜底默认）；
mcpList 返回启用的 server 清单（描述截断由调用方完成）。
*/
func NewStatus(fsys fs.FileSystem, store *Store, ctxTokens func() int, ctxWindow int,
	mcpList func() []StatusMcp) *Status {
	return &Status{fsys: fsys, store: store, ctxTokens: ctxTokens, ctxWindow: ctxWindow, mcpList: mcpList}
}

func (h *Status) Name() string { return "status" }

/* OnStart 组装状态并在用户输入前插入（startHooks 运行时末条必为本轮 input）。 */
func (h *Status) OnStart(ctx context.Context, state *types.LoopState) error {
	data := h.build(ctx)
	content := "<" + StatusTag + ">\n" + jsonOf(data) + "\n</" + StatusTag + ">"

	msg := types.Message{Role: types.RoleUser, Content: content}
	if n := len(state.Messages); n > 0 && state.Messages[n-1].Role == types.RoleUser {
		state.Messages = slices.Insert(state.Messages, n-1, msg)
	} else {
		state.Messages = append(state.Messages, msg)
	}
	state.EmitEvent(EventStatus, data)
	return nil
}

/* OnEnd 记录最近输出时间（下轮"距上次输出"用）。 */
func (h *Status) OnEnd(_ context.Context, _ *types.LoopState) error {
	h.store.SetLastOutputAt(time.Now().UnixMilli())
	return nil
}

/* build 组装状态数据并推进资源基线。 */
func (h *Status) build(ctx context.Context) StatusData {
	now := time.Now()
	data := StatusData{
		Now:       now.Format("2006-01-02 15:04"),
		CtxTokens: h.ctxTokens(),
		CtxWindow: h.ctxWindow,
	}
	if last := h.store.LastOutputAt(); last > 0 {
		data.SinceLastOutputMin = (now.UnixMilli() - last) / 60000
	}
	if h.ctxWindow > 0 && data.CtxTokens > h.ctxWindow*7/10 {
		data.SuggestCompact = true
	}

	var curSkills, curMcps []string
	if skills, err := skill.LoadDir(ctx, h.fsys, SkillsDir); err == nil {
		for _, s := range skills {
			curSkills = append(curSkills, s.Name)
		}
		sort.Strings(curSkills)
	}
	for _, m := range h.mcpList() { // 全量仅作变更基线，不进状态记录
		curMcps = append(curMcps, m.Name)
	}
	sort.Strings(curMcps)

	if prev := h.store.ResSnap(); prev != nil {
		data.Changes = append(diffNames(prev.Skills, curSkills, "skill"),
			diffNames(prev.Mcps, curMcps, "mcp")...)
	}
	h.store.SetResSnap(&ResSnapshot{Skills: curSkills, Mcps: curMcps})
	return data
}

/* diffNames 对比新旧名单产出变更记录（新增 + / 移除 -）。 */
func diffNames(oldS, newS []string, kind string) []string {
	var out []string
	for _, n := range newS {
		if !slices.Contains(oldS, n) {
			out = append(out, "+ "+kind+": "+n)
		}
	}
	for _, n := range oldS {
		if !slices.Contains(newS, n) {
			out = append(out, "- "+kind+": "+n)
		}
	}
	return out
}

func jsonOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

/* ParseStatusTag 从消息内容解析 <agent_status> 载荷（非状态记录返回 nil）。 */
func ParseStatusTag(content string) *StatusData {
	open, close := "<"+StatusTag+">", "</"+StatusTag+">"
	i := strings.Index(content, open)
	if i < 0 {
		return nil
	}
	rest := content[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return nil
	}
	var d StatusData
	if json.Unmarshal([]byte(strings.TrimSpace(rest[:j])), &d) != nil {
		return nil
	}
	return &d
}

/* LastCtxTokens 从历史尾部找最近一条状态记录的水位（旧快照无
ctxTokens 字段时的恢复兜底；找不到返回 0）。 */
func LastCtxTokens(msgs []types.Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != types.RoleUser {
			continue
		}
		if d := ParseStatusTag(msgs[i].Content); d != nil && d.CtxTokens > 0 {
			return d.CtxTokens
		}
	}
	return 0
}
