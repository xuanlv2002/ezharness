/*
reference_file 是资源引用告知 hook：带引用的轮次在用户输入前插入一条
<reference_file> user 记录，统一承载本轮引用的全部文件——附件（整
文件引用，只给路径）与文件页标注（路径+片段行号+备注）。一切皆资源：
文件本体不进上下文，模型按需 read_file 读取真身；前端历史重建按同一
标签解析引用 chips。无引用
轮次零开销（不插消息）。
*/
package hooks

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/types"
)

/* RefTag 是引用记录的包裹标签（前端历史重建按它识别）。 */
const RefTag = "reference_file"

/* MetaRefFiles 是 LoopState.Metadata 的引用键。 */
const MetaRefFiles = "reference_files"

/* RefItem 是文件内的一处标注片段（行号定位 + 备注）。 */
type RefItem struct {
	Sel  string `json:"sel"`
	Note string `json:"note"`
	From int    `json:"from"`
	To   int    `json:"to"`
}

/* RefFile 是一条文件引用：Items 空 = 整文件引用（附件），有 = 片段标注。 */
type RefFile struct {
	Path  string    `json:"path"`
	Items []RefItem `json:"items,omitempty"`
}

/* WithRefFiles 把本轮引用放进 LoopState（Run 阶段单线程写，安全）。 */
func WithRefFiles(refs []RefFile) core.RunOption {
	return func(st *types.LoopState) {
		if len(refs) > 0 {
			st.Metadata[MetaRefFiles] = refs
		}
	}
}

/* RefFileHook 实现 <reference_file> 注入。 */
type RefFileHook struct{}

/* NewRefFile 创建引用告知 hook。 */
func NewRefFile() *RefFileHook { return &RefFileHook{} }

func (h *RefFileHook) Name() string { return "reffile" }

/* maxRefSel 单条片段注入上限（按 rune 截断，全文可 read_file）。 */
const maxRefSel = 6000

/*
	OnStart 在本轮输入前插入引用记录（startHooks 运行时末条必为本轮 input）。

标签体是纯 JSON 载荷（前端 JSON.parse 重建 chips）。
*/
func (h *RefFileHook) OnStart(_ context.Context, state *types.LoopState) error {
	refs, _ := state.Metadata[MetaRefFiles].([]RefFile)
	if len(refs) == 0 {
		return nil
	}
	for i := range refs {
		for j := range refs[i].Items {
			sel := strings.ReplaceAll(refs[i].Items[j].Sel, "\r\n", "\n")
			if rs := []rune(sel); len(rs) > maxRefSel {
				refs[i].Items[j].Sel = string(rs[:maxRefSel]) + "…（已截断）"
			}
		}
	}
	payload := struct {
		Hint string    `json:"hint"`
		Refs []RefFile `json:"refs"`
	}{Hint: "用户本轮引用了以下本地文件，可用 read_file 读取", Refs: refs}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	msg := types.Message{Role: types.RoleUser, Content: "<" + RefTag + ">\n" + string(body) + "\n</" + RefTag + ">"}
	if n := len(state.Messages); n > 0 && state.Messages[n-1].Role == types.RoleUser {
		state.Messages = slices.Insert(state.Messages, n-1, msg)
	} else {
		state.Messages = append(state.Messages, msg)
	}
	return nil
}
