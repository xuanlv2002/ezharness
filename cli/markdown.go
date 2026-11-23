/*
markdown 渲染：助手回复按终端宽度渲染成带样式的块（标题/列表/代码）。
渲染器按宽度缓存，消息行按自己的宽度缓存结果，流式帧不会重复解析。
*/
package main

import (
	"fmt"
	"net/url"
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
	src, supper := rewriteSupperLinks(src)
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
	return restoreSupperLinks(linkify(strings.ReplaceAll(strings.Trim(out, "\n"), "\t", "    ")), supper)
}

/* supperLink 是 <$supper_url> 占位入口的展示文本与可点击目标（uri 空 = 不可点）。 */
type supperLink struct {
	label string
	uri   string
}

var supperURLRe = regexp.MustCompile(`<\$supper_url>\s*([\s\S]*?)\s*</\$supper_url>`)

/*
rewriteSupperLinks 把 <$supper_url>目标</$supper_url> 换成短占位符再交给
markdown 渲染——原样留着会被当成文字打出来，长路径还会被折行拆散。
渲染完再换回展示文本（见 restoreSupperLinks）。
*/
func rewriteSupperLinks(src string) (string, []supperLink) {
	var links []supperLink
	out := supperURLRe.ReplaceAllStringFunc(src, func(m string) string {
		target := supperURLRe.FindStringSubmatch(m)[1]
		links = append(links, supperLabel(target))
		return fmt.Sprintf("〔L%d〕", len(links)-1)
	})
	return out, links
}

/* supperLabel 按协议给出展示文本：http/file 可点（OSC 8），终端/浏览器/快应用只给标签。 */
func supperLabel(target string) supperLink {
	switch {
	case strings.HasPrefix(target, "http://"), strings.HasPrefix(target, "https://"):
		return supperLink{label: target, uri: target}
	case strings.HasPrefix(target, "file://"):
		return supperLink{label: fileURLPath(target), uri: target}
	case strings.HasPrefix(target, "term://"):
		return supperLink{label: "终端 " + strings.TrimPrefix(target, "term://")}
	case strings.HasPrefix(target, "browser://"):
		return supperLink{label: "浏览器 " + strings.TrimPrefix(target, "browser://")}
	case strings.HasPrefix(target, "app://"):
		return supperLink{label: "快应用 " + strings.TrimPrefix(target, "app://")}
	}
	return supperLink{label: target}
}

/* fileURLPath file:// URL 折成本地路径展示（Windows 去掉多余的前导斜杠）。 */
func fileURLPath(u string) string {
	p := strings.TrimPrefix(u, "file://")
	if unescaped, err := url.PathUnescape(p); err == nil {
		p = unescaped
	}
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return p
}

/* restoreSupperLinks 把占位符换成展示文本（可点的套 OSC 8）。 */
func restoreSupperLinks(s string, links []supperLink) string {
	for i, l := range links {
		text := l.label
		if l.uri != "" {
			text = ansi.SetHyperlink(escapeLinkURI(l.uri)) + text + ansi.ResetHyperlink()
		}
		s = strings.ReplaceAll(s, fmt.Sprintf("〔L%d〕", i), text)
	}
	return s
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
