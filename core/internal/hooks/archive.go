/*
archive 是话题归档：用户显式触发（前端按键，会话空闲时），把当前会话
总结归档并换代——旧库封存（Archived）、新库空置起步（system 重组：
base 全量重载 + <compact-summary> 摘要段 + 上一库路径引用）、话题线
换代（同线 UpdateLeaf，树的纵深）。与 trim（上下文整理，模型侧、就地
不换库）相对：归档是会话树管理，摘要输入用模型视图（含 marker 摘要链，
链式浓缩）。对模型完全透明，是"记忆翻页"。
*/
package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
)

/* EventCompact 是话题归档事件（Data 为 CompactInfo）。 */
const EventCompact = event.EventType("session.compact")

/* CompactInfo 描述一次归档。 */
type CompactInfo struct {
	OldID    string `json:"oldId"`
	NewID    string `json:"newId"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	PrevPath string `json:"prevPath"`
}

const archiveSummaryPrompt = "为切换到全新会话生成交接摘要：保留上一会话的关键事实、" +
	"已达成的决定、未完成的待办与用户偏好；忽略过程细节与状态栏记录；简洁自包含，300 字以内。"

/*
ArchiveSession 执行归档换代：摘要模型视图 → 旧库封存 → 写新库初始
快照（空 messages + 新 system + compress 边，重启可恢复形态）→ 内存
切换（sys 热更、SetID/SetPrev、trace 换库、话题线换代）。
full 是渲染全量历史（话题标题与线规模），view 是模型视图（摘要输入）。
*/
func ArchiveSession(ctx context.Context, p provider.ModelProvider, fsys fs.FileSystem,
	sess *Store, sys *SysPrompt, topics *Topics, trace *Trace,
	rebuildBase func() string, full, view []types.Message) (CompactInfo, error) {
	oldID := sess.ID()
	if oldID == "" {
		return CompactInfo{}, errors.New("no session to archive")
	}
	summaryText, err := summarizeMsgs(ctx, p, archiveSummaryPrompt, view)
	if err != nil {
		return CompactInfo{}, err
	}

	// 旧库封存：空闲触发，文件已是全量最新（含 trim 档案），只翻 Archived 位
	old, err := LoadSnap(ctx, fsys, oldID)
	if err != nil {
		return CompactInfo{}, err
	}
	old.Archived = true
	if data, merr := json.MarshalIndent(old, "", "  "); merr == nil {
		if werr := fsys.Write(ctx, SessionsDir+"/"+oldID+"/session.json", data); werr != nil {
			return CompactInfo{}, werr
		}
	}

	prevPath := SessionsDir + "/" + oldID
	base := ""
	if rebuildBase != nil {
		base = rebuildBase()
	} else if b, _ := sys.Parts(); b != "" {
		base = b
	}
	summaryBlock := "<compact-summary>\n上一会话已归档，原始记录在 " + prevPath +
		"（session.json 可读取全文）。本会话开始前的摘要：\n" + summaryText + "\n</compact-summary>"

	sys.Set(base, summaryBlock) // 新 system 两段生效（sys 是会话持有的热更实例）

	// 新库初始快照先落盘：内存切换后任一时刻重启，新库都是可恢复形态
	newID := NewSessionID()
	root := sess.LineRoot()
	now := time.Now()
	initSnap := SessionSnap{
		ID:             newID,
		CreatedAt:      now.UnixMilli(),
		Messages:       nil,
		SystemPrompt:   sys.Prompt(),
		SystemBase:     base,
		SummaryBlock:   summaryBlock,
		TargetID:       oldID,
		SeedKind:       "compress",
		LineRoot:       root,
		CompactSummary: summaryText,
		CtxWindow:      old.CtxWindow,
		StartedAt:      now,
		EndedAt:        now,
	}
	if data, merr := json.MarshalIndent(initSnap, "", "  "); merr == nil {
		if werr := fsys.Write(ctx, SessionsDir+"/"+newID+"/session.json", data); werr != nil {
			return CompactInfo{}, werr
		}
	}

	sess.SetID(newID) // 用量/折叠档案/边清零，lineRoot 保留（换代不换线）
	sess.SetPrev(oldID, summaryText)
	if trace != nil {
		trace.SetTrace(newID)
	}
	// 换代不换线名：归档未创建新分支，线标题保持身份不变（世代节点
	// 标题各自按本代首条 user，见 Tree()）；title 仅线未索引时兜底
	_ = topics.UpdateLeaf(root, newID, FirstUserTitle(full), now.UnixMilli(), len(full))

	return CompactInfo{OldID: oldID, NewID: newID, Title: FirstUserTitle(full),
		Summary: summaryText, PrevPath: prevPath}, nil
}

/* TitleFromMsg 用消息内容作标题（fork 锚点命名）：截断 40 字。 */
func TitleFromMsg(m types.Message) string {
	t := strings.TrimSpace(m.Content)
	if t == "" {
		return "未命名"
	}
	if len([]rune(t)) > 40 {
		return string([]rune(t)[:40]) + "…"
	}
	return t
}

/*
FirstUserTitle 从消息里取首条真实 user 文本作话题标题（跳过 agent_status
状态栏、res_change 资源变更、end_reason 轮次收尾与 context_trim 整理
marker——它们是 role=user 的注入消息）。
*/
func FirstUserTitle(msgs []types.Message) string {
	for _, m := range msgs {
		if m.Role != types.RoleUser || IsTrimMarker(m) {
			continue
		}
		t := strings.TrimSpace(m.Content)
		if t == "" ||
			strings.HasPrefix(t, "<agent_status>") ||
			strings.HasPrefix(t, "<"+ResChangeTag+">") ||
			strings.HasPrefix(t, "<"+EndReasonTag+">") {
			if len(m.Images) > 0 && t == "" {
				return "[图片]"
			}
			continue
		}
		if len([]rune(t)) > 40 {
			return string([]rune(t)[:40]) + "…"
		}
		return t
	}
	return "未命名话题"
}
