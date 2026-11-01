/*
工具 schema 守卫：ArgsSchema 会被直接塞进模型请求的 tools 字段（json.RawMessage），
非法 JSON 会让每次请求在本地 marshal 阶段立刻失败并进入无谓重试（2026-10 事故：
model_call 拼接多了一个 '}'，表现为"发消息没回复、界面显示重试"）。
新增/修改任何工具 schema 后必须过这个测试。
*/
package tools

import (
	"encoding/json"
	"testing"

	"ezharness/core/internal/osfs"
	"github.com/xuanlv2002/ezloop/types"
)

func TestAllToolSchemasValidJSON(t *testing.T) {
	var list []types.Tool
	list = append(list, SaveApp(osfs.OS{})...)
	list = append(list, Sleep(), ModelCall(osfs.OS{}, nil, nil, nil))
	list = append(list, Memory(osfs.OS{}), Plan(osfs.OS{}, "t", nil))
	list = append(list, LoadApp(osfs.OS{}), Callback(nil, func() string { return "" }))
	list = append(list, SharedTerm(nil)...)
	list = append(list, SharedBrowser(nil)...)
	for _, tl := range list {
		s := tl.ArgsSchema()
		if len(s) == 0 {
			continue
		}
		if !json.Valid(s) {
			t.Errorf("%s ArgsSchema 非法 JSON：%s", tl.Name(), s)
		}
	}
}
