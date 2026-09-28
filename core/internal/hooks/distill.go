/*
distill 是归档的沉淀步：归档换代前，把会话中值得跨会话长期保留的信息
融合进记忆文件。一次模型调用：全量旧文（user/soul/项目索引/各项目文件）
+ 会话 → 按 ===段名=== 分段输出各文件新全文，代码解析段名覆盖写回
（UNCHANGED 或与现文件相同跳过）。无 JSON 中转、无多阶段管线——
记忆更新统一为"读旧全文 → 融合 → 写新全文"。

失败静默降级（归档仍继续）：沉淀是增值动作，不该挡住用户显式触发的
归档；调用侧拿不到 LoopState，不落 trace span。
*/
package hooks

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
)

/* distillFiles 固定记忆文件段名（project:<id> 段动态，另行解析）。 */
var distillFiles = [2]string{"user", "soul"}

const distillPrompt = "你在维护一个 agent 的长期记忆。下面是刚结束的一段会话、当前工作目录与全部记忆文件的当前内容。\n" +
	"读旧全文 → 融合会话中值得跨会话保留的信息 → 输出文件新全文（结构自定，小节/段落皆可；" +
	"同一事实新表述覆盖旧的，过时信息修正或删除）。\n" +
	"文件与提取范围：\n" +
	"- user：用户个人信息——偏好与习惯、职业背景、工作环境、固定的表达与流程约定（≤30 行）\n" +
	"- soul：agent 工作习惯——本环境下顺手的做法、工具与验证方式偏好、与该用户协作的有效模式（≤30 行）\n" +
	"- project:<id>：项目记忆——背景/约定/关键路径/交付物位置（≤40 行，markdown 首行 '# <项目名>'）。" +
	"仅当会话围绕明确项目展开时输出；<id> 为小写英文数字连字符（优先工作目录名）；" +
	"输入含该项目旧文则融合更新，否则新建\n" +
	"- project.md：项目索引，一行一项目「- 项目名 — 项目描述 | 记忆: %s/project-<id>.md」（地址用这个绝对前缀拼，模型直接按它 read_file）；" +
	"输出 project:<id> 段时同步输出（其他项目行原样保留，不造重复行）\n" +
	"不要提取：一次性任务过程、临时性决定、可从代码或文档中恢复的技术细节。\n" +
	"无新内容的文件段输出 UNCHANGED；会话与项目无关时 project:<id> 与 project.md 段都不输出。\n" +
	"输出格式（严格遵守，除文件段外不输出任何说明文字）：\n" +
	"===user===\n（user.md 新全文或 UNCHANGED）\n===soul===\n（soul.md 新全文或 UNCHANGED）\n" +
	"===project:<id>===\n（项目文件新全文）\n===project.md===\n（project.md 新全文）\n\n" +
	"【工作目录】%s\n\n【记忆文件当前内容】"

/*
distillToMemory 执行沉淀：组装全量旧文（user/soul/project.md + 各 project-*.md）
→ 一次模型调用 → 解析分段 → 覆盖写回。user/soul 由代码补 '# <名>' 文件头，
项目文件与索引原样写入。view 是模型视图（与归档摘要同源，含 marker 链）。
*/
func distillToMemory(ctx context.Context, p provider.ModelProvider, fsys fs.FileSystem, view []types.Message, workDir string) error {
	ctx, cancel := context.WithTimeout(ctx, modelSummaryTimeout)
	defer cancel()
	var b strings.Builder
	ltAbs, err := filepath.Abs(LongtermDir) // 索引里的记忆地址给绝对路径：模型不知道 FS 挂载基准
	if err != nil {
		ltAbs = LongtermDir
	}
	b.WriteString(fmt.Sprintf(distillPrompt, filepath.ToSlash(ltAbs), workDir))
	current := map[string]string{}
	for _, name := range distillFiles {
		b.WriteString("\n=== " + name + " ===\n")
		if data, err := fsys.Read(ctx, LongtermDir+"/"+name+".md"); err == nil {
			current[name] = string(data)
			b.Write(data)
		} else {
			b.WriteString("（尚无此文件）")
		}
	}
	b.WriteString("\n=== project.md ===\n")
	if data, err := fsys.Read(ctx, ProjectMd); err == nil {
		current["project.md"] = string(data)
		b.Write(data)
	} else {
		b.WriteString("（尚无此文件）")
	}
	if entries, err := fsys.List(ctx, LongtermDir); err == nil {
		for _, e := range entries {
			if e.IsDir || !strings.HasPrefix(e.Name, "project-") || !strings.HasSuffix(e.Name, ".md") {
				continue
			}
			id := strings.TrimSuffix(strings.TrimPrefix(e.Name, "project-"), ".md")
			data, rerr := fsys.Read(ctx, LongtermDir+"/"+e.Name)
			if rerr != nil {
				continue
			}
			current["project:"+id] = string(data)
			b.WriteString("\n=== project:" + id + " ===\n")
			b.Write(data)
		}
	}
	resp, err := p.Invoke(ctx, &types.ModelRequest{Messages: []types.Message{
		{Role: types.RoleUser, Content: b.String() + "\n\n【刚结束的会话】\n" + flatMsgs(view)},
	}})
	if err != nil {
		return err
	}
	for key, content := range parseDistillSections(resp.Content) {
		content = strings.TrimSpace(content)
		if content == "" || content == "UNCHANGED" {
			continue
		}
		switch {
		case key == "user" || key == "soul":
			body := "# " + key + "\n\n" + content + "\n"
			if body == current[key] {
				continue
			}
			if werr := fsys.Write(ctx, LongtermDir+"/"+key+".md", []byte(body)); werr != nil {
				return werr
			}
		case key == "project.md":
			if content+"\n" == current["project.md"] {
				continue
			}
			if werr := fsys.Write(ctx, ProjectMd, []byte(content+"\n")); werr != nil {
				return werr
			}
		case strings.HasPrefix(key, "project:"):
			id := strings.TrimPrefix(key, "project:")
			body := content + "\n"
			if body == current[key] {
				continue
			}
			if werr := fsys.Write(ctx, LongtermDir+"/project-"+id+".md", []byte(body)); werr != nil {
				return werr
			}
		}
	}
	return nil
}

/*
parseDistillSections 解析 ===name=== 分段输出（行首整行匹配）。合法段名：
user / soul / project.md / project:<id>（id 经 projectSlug 规范化，空则丢弃）；
其余段名丢弃（防模型自造段名写歪文件）。返回段名 → 段内容。
*/
func parseDistillSections(text string) map[string]string {
	out := map[string]string{}
	cur := ""
	var b strings.Builder
	closeSection := func() {
		if cur != "" {
			out[cur] = b.String()
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "===") && strings.HasSuffix(trimmed, "===") {
			closeSection()
			b.Reset()
			cur = distillSectionName(strings.Trim(trimmed, "= "))
			continue
		}
		if cur != "" {
			b.WriteString(line + "\n")
		}
	}
	closeSection()
	return out
}

/* distillSectionName 校验并规范化段名，非法返回空串。 */
func distillSectionName(name string) string {
	switch name {
	case "user", "soul", "project.md":
		return name
	}
	if id := projectSlug(strings.TrimPrefix(name, "project:")); id != "" && name != "project:" {
		return "project:" + id
	}
	return ""
}

/* flatMsgs 把消息压平为 "role: content" 文本（与 summarizeMsgs 同拼装）。 */
func flatMsgs(msgs []types.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(string(m.Role) + ": " + m.Content + "\n")
	}
	return b.String()
}

/*
projectSlug 规范项目标识：小写、空白与下划线转连字符、去文件名非法
字符（保留中英文数字与连字符），压缩连续连字符，截 64 字符。
*/
func projectSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-',
			r >= 0x4e00 && r <= 0x9fff:
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '\t':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
