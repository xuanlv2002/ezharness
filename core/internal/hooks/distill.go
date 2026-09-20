/*
distill 是归档的沉淀步：归档换代前，把会话中值得跨会话长期保留的信息
合并进三个固定记忆文件（user/projects/lessons），再做交接摘要——归档是
"总结沉淀"而不只是压缩。固定文件集 + 合并重写式更新（模型看全量旧内容
与新内容整合后整体覆盖），多次归档不会冲突膨胀，也不开新文件。

失败静默降级（归档仍继续）：沉淀是增值动作，不该挡住用户显式触发的
归档；调用侧拿不到 LoopState，不落 trace span。
*/
package hooks

import (
	"context"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
)

/* distillFiles 固定记忆文件集（文件名即主题，不动态开新 topic）。 */
var distillFiles = [3]string{"user", "projects", "lessons"}

const distillPrompt = "你在维护一个 agent 的长期记忆文件。下面是刚结束的一段会话与三个记忆文件的当前内容，" +
	"从会话中提取值得跨会话长期保留的信息，合并进文件后输出三个文件的新全文。\n" +
	"提取范围（各文件只收满足属性的内容）：\n" +
	"- user：用户偏好与习惯、稳定的用户事实（职业背景、工作环境、固定的表达与流程约定）\n" +
	"- projects：项目与任务背景（在做什么、关键路径与约定、交付物位置）\n" +
	"- lessons：踩过的坑与解法、验证过的环境特性、可复用的经验\n" +
	"不要提取：一次性任务过程、临时性决定、可从文件中恢复的技术细节。\n" +
	"合并规则：同一事实新表述覆盖旧表述，过时信息修正或删除；每个文件保持 30 行以内，" +
	"超出按重要性截断；每条自包含一行；某文件无值得合并的新内容时该文件段只输出 UNCHANGED。\n" +
	"输出格式（严格遵守，除三个文件段外不输出任何说明文字）：\n" +
	"===user===\n（user.md 新全文或 UNCHANGED）\n===projects===\n（projects.md 新全文或 UNCHANGED）\n" +
	"===lessons===\n（lessons.md 新全文或 UNCHANGED）\n\n【三个记忆文件当前内容】"

/*
distillToMemory 执行沉淀：组装现有文件内容 → 一次模型调用 → 解析三段
→ 覆盖写回（UNCHANGED 或与现文件相同跳过）。view 是模型视图（与归档
摘要同源，含 marker 链）。
*/
func distillToMemory(ctx context.Context, p provider.ModelProvider, fsys fs.FileSystem, view []types.Message) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var b strings.Builder
	b.WriteString(distillPrompt)
	current := map[string]string{}
	for _, name := range distillFiles {
		path := LongtermDir + "/" + name + ".md"
		b.WriteString("\n=== " + name + " ===\n")
		if data, err := fsys.Read(ctx, path); err == nil {
			current[name] = string(data)
			b.Write(data)
		} else {
			b.WriteString("（尚无此文件）")
		}
	}
	resp, err := p.Invoke(ctx, &types.ModelRequest{Messages: []types.Message{
		{Role: types.RoleUser, Content: b.String() + "\n\n【刚结束的会话】\n" + flatMsgs(view)},
	}})
	if err != nil {
		return err
	}
	sections := parseDistillSections(resp.Content)
	for _, name := range distillFiles {
		content, ok := sections[name]
		if !ok {
			continue
		}
		content = strings.TrimSpace(content)
		if content == "" || content == "UNCHANGED" {
			continue
		}
		body := "# " + name + "\n\n" + content + "\n"
		if body == current[name] {
			continue
		}
		if werr := fsys.Write(ctx, LongtermDir+"/"+name+".md", []byte(body)); werr != nil {
			return werr
		}
	}
	return nil
}

/*
parseDistillSections 解析 ===name=== 分段输出（行首整行匹配，只认固定
文件名，防模型自造段名写歪文件）。返回 name → 段内容。
*/
func parseDistillSections(text string) map[string]string {
	out := map[string]string{}
	cur := ""
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "===") && strings.HasSuffix(trimmed, "===") {
			name := strings.Trim(trimmed, "= ")
			if cur != "" {
				out[cur] = b.String()
			}
			if name == "user" || name == "projects" || name == "lessons" {
				cur = name
			} else {
				cur = "" // 未知段名：内容丢弃
			}
			b.Reset()
			continue
		}
		if cur != "" {
			b.WriteString(line + "\n")
		}
	}
	if cur != "" {
		out[cur] = b.String()
	}
	return out
}

/* flatMsgs 把消息压平为 "role: content" 文本（与 summarizeMsgs 同拼装）。 */
func flatMsgs(msgs []types.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(string(m.Role) + ": " + m.Content + "\n")
	}
	return b.String()
}
