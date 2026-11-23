/*
markdown 渲染：助手回复按终端宽度渲染成带样式的块（标题/列表/代码）。
渲染器按宽度缓存，消息行按自己的宽度缓存结果，流式帧不会重复解析。
*/
package main

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
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
	src = cleanText(src)
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
	return linkify(strings.ReplaceAll(strings.Trim(out, "\n"), "\t", "    "))
}

/* mdURL 匹配渲染结果里的裸 URL（不含 ANSI 序列与空白）。 */
var mdURL = regexp.MustCompile(`https?://[^\s\x1b\x07]+`)

/*
linkify 给 URL 套 OSC 8 超链接：支持的终端（Windows Terminal、iTerm2 等）
里 Ctrl/⌘+点击可直达；不支持的终端只是忽略该转义，显示不变。链接末尾的
句读/右括号不算 URL（markdown 里常紧跟标点）。
*/
func linkify(s string) string {
	return mdURL.ReplaceAllStringFunc(s, func(u string) string {
		tail := ""
		if trimmed := strings.TrimRight(u, ".,;:!?)]}>，。；：！？）】"); trimmed != "" {
			tail = u[len(trimmed):]
			u = trimmed
		}
		return ansi.SetHyperlink(escapeLinkURI(u)) + u + ansi.ResetHyperlink() + tail
	})
}

/* escapeLinkURI 去掉会截断 OSC 8 的终止符（BEL/ESC）。 */
func escapeLinkURI(u string) string {
	return strings.NewReplacer("\x07", "", "\x1b", "").Replace(u)
}

/* markdownLines 给渲染结果加上 AI 前缀与续行缩进，并硬截到行宽内。 */
func markdownLines(md string, width int) []string {
	lines := strings.Split(strings.Trim(md, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for i, ln := range lines {
		if i == 0 {
			ln = "AI  " + ln
		} else {
			ln = "    " + ln
		}
		out = append(out, ansi.Truncate(ln, width-1, ""))
	}
	return out
}
