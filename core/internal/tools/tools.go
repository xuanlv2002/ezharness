/*
Package tools 提供 ezharness 自有的增量工具：save_app（快应用生成）。
read_file / write_file / edit_file / terminal 复用 ezloop 的 filetools
hook（原生 shell 执行，Windows 为 cmd），在 agent_service 装配。
*/
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/osfs"
)

/* appsDir 是快应用存放目录。 */
const appsDir = "apps"

/* SaveApp 返回快应用工具集。 */
func SaveApp(fsys osfs.OS) []types.Tool {
	return []types.Tool{saveAppTool{fsys}}
}

type saveAppTool struct{ fsys osfs.OS }

func (saveAppTool) Name() string { return "save_app" }
func (saveAppTool) Description() string {
	return "把一个自包含的 html 小工具保存为快应用（用户可在快应用页一键启动）。name 用英文短名，html 是完整文档"
}

func (t saveAppTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "应用名（英文短名，如 pomodoro）"},
			"html": {"type": "string", "description": "完整 html 文档内容（自包含，内联 css/js）"}
		},
		"required": ["name", "html"]
	}`)
}

func (t saveAppTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Name string `json:"name"`
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	name := strings.TrimSuffix(strings.TrimSpace(a.Name), ".html")
	if name == "" || strings.ContainsAny(name, `/\:*?"<>|`) {
		return "", errors.New("invalid app name")
	}
	file := name + ".html"
	if err := t.fsys.Write(ctx, appsDir+"/"+file, []byte(a.HTML)); err != nil {
		return "", err
	}
	return fmt.Sprintf("快应用已保存：%s（用户可在快应用页启动，URL /apps/%s）", file, file), nil
}
