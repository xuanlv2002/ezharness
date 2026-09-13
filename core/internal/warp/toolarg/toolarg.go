/*
Package toolarg 是工具参数语法糖装饰器：参数字符串值里的
<@toolArg>路径</@toolArg> 标签在执行前展开为文件内容（递归遍历
JSON 的字符串值，Marshal 回填保证转义安全）。

只影响执行：入史的 assistant ToolCall.Args 与工具卡展示的参数仍是
标签原文——模型少一次 read_file 往返，上下文不被重复内容撑大。
展开失败（文件不存在/超上限）作为工具错误回传模型自纠。
*/
package toolarg

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/types"
	"github.com/xuanlv2002/ezloop/warp"
)

/* tagRe 匹配语法糖标签（非贪婪：到最近的闭合标签止）。 */
var tagRe = regexp.MustCompile(`(?s)<@toolArg>(.*?)</@toolArg>`)

/* maxExpandChars 单文件展开上限（与 read_file 单次字符上限对齐）。 */
const maxExpandChars = 200_000

/* Warp 返回工具中间件：参数含标签时先展开再执行。 */
func Warp(fsys fs.FileSystem) warp.ToolHandler {
	return func(_ event.Emitter, inner types.Tool) types.Tool {
		return &toolArgTool{inner: inner, fsys: fsys}
	}
}

type toolArgTool struct {
	inner types.Tool
	fsys  fs.FileSystem
}

func (t *toolArgTool) Name() string                { return t.inner.Name() }
func (t *toolArgTool) Description() string         { return t.inner.Description() }
func (t *toolArgTool) ArgsSchema() json.RawMessage { return t.inner.ArgsSchema() }

func (t *toolArgTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	if !strings.Contains(string(args), "<@toolArg>") {
		return t.inner.Invoke(ctx, args)
	}
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		return t.inner.Invoke(ctx, args) // 非常规 JSON：原样透传，交由内层处理
	}
	// 两遍：先收集引用并验证可读（失败即返回模型可读的错误），再替换
	paths := collectRefs(v)
	contents := make(map[string]string, len(paths))
	for _, p := range paths {
		data, err := t.fsys.Read(ctx, p)
		if err != nil {
			return "", fmt.Errorf("toolArg 引用无法读取：%s（%v）", p, err)
		}
		if n := utf8.RuneCountInString(string(data)); n > maxExpandChars {
			return "", fmt.Errorf("toolArg 引用 %s 共 %d 字符，超出 %d 上限（请分次或改用 read_file）", p, n, maxExpandChars)
		}
		contents[p] = string(data)
	}
	out, err := json.Marshal(expand(v, contents))
	if err != nil {
		return "", fmt.Errorf("toolarg: 重新序列化失败: %w", err)
	}
	return t.inner.Invoke(ctx, out)
}

/* collectRefs 递归收集全部标签引用路径（保序去重；空路径跳过）。 */
func collectRefs(v any) []string {
	var out []string
	var walk func(any)
	walk = func(cur any) {
		switch c := cur.(type) {
		case map[string]any:
			for _, val := range c {
				walk(val)
			}
		case []any:
			for _, val := range c {
				walk(val)
			}
		case string:
			for _, m := range tagRe.FindAllStringSubmatch(c, -1) {
				p := strings.TrimSpace(m[1])
				if p != "" && !slices.Contains(out, p) {
					out = append(out, p)
				}
			}
		}
	}
	walk(v)
	return out
}

/*
	expand 递归替换：字符串里的标签替换为 contents 路径对应的内容

（collectRefs 已保证命中；空路径保留原文由模型自纠）。
*/
func expand(v any, contents map[string]string) any {
	switch cur := v.(type) {
	case map[string]any:
		for k, val := range cur {
			cur[k] = expand(val, contents)
		}
		return cur
	case []any:
		for i, val := range cur {
			cur[i] = expand(val, contents)
		}
		return cur
	case string:
		return tagRe.ReplaceAllStringFunc(cur, func(sub string) string {
			p := strings.TrimSpace(tagRe.FindStringSubmatch(sub)[1])
			if c, ok := contents[p]; ok {
				return c
			}
			return sub
		})
	default:
		return v
	}
}
