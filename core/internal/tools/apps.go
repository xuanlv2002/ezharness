/*
快应用的 agent 侧工具：load_app 读应用文档。应用清单与增删改和技能
同构——清单注入 system 的 <apps> 段，轮内变化由 remind 的
<resource_change> 报告；不提供 list_apps（技能也没有 list_skills）。

应用能不能被 agent 调用不由工具判断——只看它自己的文档有没有
「Agent 调用」一节；具体怎么调（JS / HTTP / CLI）也由文档定义，
agent 按文档用 browser / terminal 等现有工具执行。
*/
package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/osfs"
	"ezharness/core/internal/quickapp"
)

type loadAppArgs struct {
	Name string `json:"name" desc:"应用目录名（list_apps 可查）"`
}

/* LoadApp 读取应用元数据、调用文档与版本历史。 */
func LoadApp(fsys osfs.OS) types.Tool {
	return types.NewTool("load_app",
		"读取快应用的元数据、调用文档与版本历史（CHANGELOG.md）。"+
			"使用某个应用前先调用它：文档含「Agent 调用」一节才可按文档调用（前端 JS 用 browser_action eval，HTTP/CLI 用 terminal 或文档指定方式）；"+
			"没有该节表示应用只面向用户，不要臆造调用方式",
		func(ctx context.Context, in *loadAppArgs) (string, error) {
			name := strings.TrimSpace(in.Name)
			info, err := quickapp.Load(ctx, fsys, name)
			if errors.Is(err, quickapp.ErrNotFound) {
				return "", fmt.Errorf("无快应用 %q（用 list_apps 查清单）", name)
			}
			if err != nil {
				return "", err
			}
			return renderAppDocument(ctx, fsys, info), nil
		})
}

/* renderAppDocument 拼装 load_app 的返回文本。 */
func renderAppDocument(ctx context.Context, fsys osfs.OS, info quickapp.Info) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s（%s）", info.Title, info.Name)
	if info.Version != "" {
		b.WriteString(" v" + info.Version)
	}
	if info.Description != "" {
		b.WriteString("\n" + info.Description)
	}
	b.WriteString("\nentry: " + info.Entry)
	if info.Backend != "" {
		b.WriteString("\nbackend: " + info.Backend)
	}
	if len(info.Callbacks) > 0 {
		b.WriteString("\n定时回调端点（callback 工具按 name 引用）：")
		for _, c := range info.Callbacks {
			line := "\n- " + c.Name
			if c.Title != "" {
				line += "（" + c.Title + "）"
			}
			line += " " + c.Method + " " + c.URL
			if c.Description != "" {
				line += " — " + c.Description
			}
			b.WriteString(line)
			if len(c.Body) > 0 {
				b.WriteString("\n  body: " + oneLineJSON(c.Body))
			}
		}
	}
	if info.Document == "" {
		b.WriteString("\n调用文档：无——该应用仅面向用户，不要臆造接口；用户要求时可用浏览器操作页面。")
	} else {
		b.WriteString("\n调用文档：" + info.Document)
		if info.Callable {
			b.WriteString("（含「Agent 调用」一节，按文档执行）")
		} else {
			b.WriteString("（未发现「Agent 调用」一节——该应用仅面向用户，不要臆造接口）")
		}
		if data, err := fsys.Read(ctx, quickapp.AppsDir+"/"+info.Name+"/"+info.Document); err == nil {
			b.WriteString("\n\n" + strings.TrimSpace(string(data)))
		}
	}
	changelog := quickapp.AppsDir + "/" + info.Name + "/CHANGELOG.md"
	if data, err := fsys.Read(ctx, changelog); err == nil {
		if text := strings.TrimSpace(string(data)); text != "" {
			b.WriteString("\n\n# 版本历史（CHANGELOG.md）\n" + text)
		}
	}
	return b.String()
}

/* oneLineJSON 把回调声明的 body 压成单行并截断（load_app 结果可读、不撑上下文）。 */
func oneLineJSON(raw []byte) string {
	s := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r", ""), "\n", " ")
	if r := []rune(s); len(r) > 500 {
		return string(r[:500]) + "…"
	}
	return s
}
