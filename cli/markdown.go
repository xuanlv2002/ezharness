/*
markdown 渲染：助手回复按终端宽度渲染成带样式的块（标题/列表/代码）。
渲染器按宽度缓存，消息行按自己的宽度缓存结果，流式帧不会重复解析。
*/
package main

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
)

var (
	mdRender *glamour.TermRenderer
	mdWidth  int
)

/* renderMarkdown 渲染 markdown；失败或宽度太窄返回空串（调用方回落纯文本）。 */
func renderMarkdown(src string, width int) string {
	if width < 20 {
		return ""
	}
	if mdRender == nil || mdWidth != width {
		cfg := styles.DarkStyleConfig
		zero := uint(0)
		cfg.Document.Margin = &zero
		cfg.Document.Color = nil // 正文用终端默认色，不整体压成灰
		r, err := glamour.NewTermRenderer(glamour.WithStyles(cfg), glamour.WithWordWrap(width))
		if err != nil {
			return ""
		}
		mdRender, mdWidth = r, width
	}
	out, err := mdRender.Render(src)
	if err != nil {
		return ""
	}
	return strings.Trim(out, "\n")
}

/* markdownLines 给渲染结果加上 AI 前缀与续行缩进。 */
func markdownLines(md string) []string {
	lines := strings.Split(strings.Trim(md, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for i, ln := range lines {
		if i == 0 {
			out = append(out, "AI  "+ln)
		} else {
			out = append(out, "    "+ln)
		}
	}
	return out
}
