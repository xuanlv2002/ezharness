/*
systemprompt 拥有 system base 的全部段文本。分层契约（防漂移的根）：

一事实一归属——每段一个函数，段文本只住在自己的函数里，改哪段进哪个
函数；跨段只允许标签名引用（"纪律见 <action>"），不允许内容复述。
工具机制不进 base：terminal、term_ 系、browser_ 系、sleep 的用法在
各自工具描述里（随工具代码更新），OpenAPI 用法在 open-api 技能，
trim 等另有 tool-guide——改这些永远不用回来改 base。

段序固定：开篇身份 → SystemExtra → <workspace> 环境 → <memory> 记忆 →
<skills>/<apps>/<models>/<mcp> 资源清单 → <local_api> 本机 API →
<action> 行动准则 → <output> 输出规范。base 只在 session 创建时组装一次
（同 session 不变）；skill 全文与记忆细节不注入（模型按需用文件工具读取），
清单变更要等下个 session 才进 system，期间由 remind 变更段的
<resource_change> 告知模型。
*/
package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"ezharness/core/internal/builtinskill"
	"ezharness/core/internal/callback"
	"ezharness/core/internal/config"
	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
	"ezharness/core/internal/osfs"

	"github.com/xuanlv2002/ezloop/ext/hook/skill"
)

/* buildSystemBase 按 fixed 段序拼装 base，空段（未配置/无清单）自动跳过。 */
func buildSystemBase(ctx context.Context, st domain.Settings, fsys osfs.OS, mc domain.ModelsConfig, sessionID string) string {
	dataDir, _ := os.Getwd() // 进程 cwd 即数据目录（启动时 chdir）
	workDir := ResolveWorkDir(st.WorkDir)
	sections := []string{
		openingSection(),
		st.SystemExtra,
		workspaceSection(dataDir, workDir),
		memorySection(ctx, fsys),
		skillsSection(ctx, fsys, st, dataDir),
		renderAppsSection(ctx, fsys),
		callbacksSection(ctx, fsys, sessionID),
		modelsSection(mc),
		mcpSection(fsys),
		localAPISection(),
		actionSection(),
		outputSection(),
	}
	out := make([]string, 0, len(sections))
	for _, s := range sections {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n\n")
}

/*
openingSection 开篇：身份一句 + 分段地图一句。具体规则零复述——
行动归 <action>，输出归 <output>，资源入口归各清单段。
*/
func openingSection() string {
	return "你是 ezharness——持续陪伴用户的设备级 agent，可全权操作本机文件与命令，能用工具就直接动手。\n" +
		"分段地图：<workspace> 环境与路径纪律 / <memory> 长期记忆 / <skills>·<apps>·<callbacks>·<models>·<mcp> 资源清单 / " +
		"<local_api> 本机 API / <action> 行动准则 / <output> 输出规范。"
}

/*
workspaceSection 环境段：数据目录、三个可写位置、路径纪律（全局硬约束，
唯一归属）、禁区。记忆不是可写位置（读写只走 memory 工具，纪律在 <memory>），
故此处不列记忆路径。
*/
func workspaceSection(dataDir, workDir string) string {
	p := func(rel string) string { return filepath.ToSlash(filepath.Join(dataDir, rel)) }
	return "<workspace>\n" +
		"# 数据目录：" + filepath.ToSlash(dataDir) + "（mcp.json、memory/、sessions/ 的根；本块路径均为绝对路径，直接使用）\n" +
		"# 可写位置：\n" +
		"# - " + filepath.ToSlash(workDir) + "  工作目录——草稿/脚本/命令产物一律放这里（terminal 默认执行目录）；其下 tmp/ 是上传附件暂存处，read_file 按路径读取（图片会作为图片消息进入上下文）\n" +
		"# - " + p("memory/skills") + "  技能库——每技能一个子目录（SKILL.md 指令 + scripts/ 脚本），新建后下个 session 进清单\n" +
		"# - " + p("apps") + "  快应用——apps/<名>/，构建约定见 quick-app 技能\n" +
		"# 路径纪律：文件读写与命令一律绝对路径；用户给了路径就把检索与读写限定在该路径内，没给路径就只在上面的可写位置内；" +
		"禁止对盘符根、用户主目录、桌面等宽泛位置做递归搜索或遍历（全盘 find/grep 极易超时卡死）；缺少路径信息先问用户，不要自行上溯目录猜路径；临时文件不要丢在工作目录外\n" +
		"# 禁区：数据目录下的配置与索引文件（settings、models、stats、topics、callbacks 等）由应用管理，不要改写；" +
		"其他历史会话的存档不要主动翻阅，确有需要先问用户；超长工具结果会被系统自动卸载为文件，按工具结果内的提示取回，不要主动浏览卸载区\n" +
		"</workspace>"
}

/*
memorySection 记忆段：结构说明 + user/soul 条目 + 项目索引 + 读写纪律。
记忆相关规则与结构的唯一归属。
*/
func memorySection(ctx context.Context, fsys osfs.OS) string {
	userItems, _ := hooks.LoadItems(ctx, fsys, hooks.UserJson)
	soulItems, _ := hooks.LoadItems(ctx, fsys, hooks.SoulJson)
	projects, _ := hooks.LoadProjects(ctx, fsys)
	var projLines strings.Builder
	if len(projects) == 0 {
		projLines.WriteString("（暂无）")
	} else {
		for _, pr := range projects {
			projLines.WriteString("- " + pr.ID + " — " + pr.Name + " — " + pr.Description + "\n")
		}
	}
	return "<memory>\n" +
		"# 长期记忆（条目式，按更新时间新→旧排；[id] 标注；条目后的「更新于 YYYY-MM-DD」是最近更新时间——" +
		"与当前时间（每轮见 <agent_status> 的「当前时间」）比对，间隔过久的当作可能已漂移的旧信息；项目详情按 id 用 memory 工具 project-read 读取）\n" +
		"# user（用户个人信息）\n" + renderMemoryItems(userItems) +
		"\n# soul（你的灵魂：用户对你的期望——性格、表达方式、工作方式与协作原则；项目相关的硬性约束写项目记忆）\n" + renderMemoryItems(soulItems) +
		"\n# project（项目索引：id — 名称 — 描述）\n" + strings.TrimSuffix(projLines.String(), "\n") +
		"\n# 读写纪律（现行，优先级最高）\n" +
		"# - 记忆只能用 memory 工具读写：条目 add/update/remove（target=user|soul，id 用上面的标注），项目 project-save/project-remove/project-read；不可用文件工具读写 memory/ 下任何文件\n" +
		"# - 提交的变更先存待确认区，用户在通知栏确认后才生效（取消即丢弃）：同一条信息不要重复提交，生效与否以下一轮 resource_change 为准\n" +
		"# - 发现值得长期保留的用户信息、或用户对你（soul）的要求随手提交；过时条目随手提交 remove，宁精勿杂\n" +
		"# - 归档时系统也会自动生成记忆修改方案（同样待用户确认）\n" +
		"\n</memory>"
}

/*
skillsSection 技能清单段：清单 + 使用入口（load_skill）+ 维护入口
（SKILL.md 可编辑）+ 停用名单。发现纪律（以此为准、不读目录）唯一归属。
无技能返回空。
*/
func skillsSection(ctx context.Context, fsys osfs.OS, st domain.Settings, dataDir string) string {
	skills := builtinskill.Skills()
	if user, err := skill.LoadDir(ctx, fsys, hooks.SkillsDir); err == nil {
		// 与内建同名的用户技能让位（内建优先），清单与记忆页同一归置
		for _, sk := range user {
			if !slices.ContainsFunc(skills, func(b skill.Skill) bool { return hooks.SkillDirOf(b.Path) == hooks.SkillDirOf(sk.Path) }) {
				skills = append(skills, sk)
			}
		}
	}
	if len(skills) == 0 {
		return ""
	}
	memRoot := filepath.ToSlash(filepath.Join(dataDir, "memory"))
	var b strings.Builder
	b.WriteString("<skills>\n（技能清单，以此为准，不要读取 skills 目录来发现；使用前先 load_skill 获取完整指令与脚本路径。" +
		"正文在 " + memRoot + "/skills/<名>/SKILL.md，可用文件工具编辑，改动下个 session 生效；" +
		"轮内变更（新增/移除/启用/停用）见 <resource_change>。标注〔内建〕的技能随应用分发，正文同样可 load_skill 查看）")
	var offNames []string
	for _, sk := range skills {
		if slices.Contains(st.DisabledSkills, hooks.SkillDirOf(sk.Path)) {
			offNames = append(offNames, sk.Name)
			continue
		}
		desc := sk.Description
		if desc == "" {
			desc = "（无描述）"
		}
		mark := ""
		if builtinskill.IsBuiltin(hooks.SkillDirOf(sk.Path)) {
			mark = "〔内建〕"
		}
		fmt.Fprintf(&b, "\n- %s%s: %s", sk.Name, mark, desc)
	}
	// 停用的只报名字（省上下文）：存在但 load_skill 会拒绝，模型不去调用也不会翻目录找
	if len(offNames) > 0 {
		slices.Sort(offNames)
		b.WriteString("\n已停用（存在但不可用，load_skill 会拒绝，不要尝试；用户需要时会在记忆页启用）：" +
			strings.Join(offNames, "、"))
	}
	b.WriteString("\n</skills>")
	return b.String()
}

/*
callbacksSection 定时回调清单段：任务标题/计划/目标/运行态 + 当前会话标注。
清单为空时整段缺席（与 mcp 同规）；创建与管理入口唯一归属 callback 工具。
*/
func callbacksSection(ctx context.Context, fsys osfs.OS, sessionID string) string {
	tasks := callback.LoadTasks(ctx, fsys)
	if len(tasks) == 0 {
		return ""
	}
	topics := hooks.NewTopics(fsys).Load()
	var b strings.Builder
	b.WriteString("<callbacks>\n（定时回调清单，以此为准，不要读取 callbacks.json 来发现；用 callback 工具 list/create/update/delete/enable/disable/run 管理。" +
		"计划语法：once {at} / interval {every} / cron \"分 时 日 月 周\"（本机时区）；会话任务绑定创建时的当前会话，到点若正在跑轮由 chat 发送逻辑先打断，归档/compact 换代后该任务失败且不会继承到新会话；快应用任务触发时自动启动后端并调用声明端点；脚本任务在工作目录执行 shell 命令；轮内变更见 <resource_change>）")
	for _, t := range tasks {
		kind := "会话"
		switch t.Kind {
		case callback.KindApp:
			kind = "快应用"
		case callback.KindScript:
			kind = "脚本"
		}
		line := "\n- [" + t.ID + "] " + briefSeg(t.Title, 80) + "（" + kind + " · " + t.Schedule.Summary() + " · "
		switch t.Kind {
		case callback.KindSession:
			if t.Session == nil {
				line += "目标缺失"
				break
			}
			if t.Session.Mode == callback.ModeNew {
				line += "新建会话"
			} else if t.Session.SessionID == "" {
				line += "会话未绑定"
			} else {
				title := t.Session.SessionID
				for _, e := range topics {
					if (e.LeafID == t.Session.SessionID || e.ID == t.Session.SessionID) && e.Title != "" {
						title = e.Title
						break
					}
				}
				line += "会话 " + briefSeg(title, 60)
				if t.Session.SessionID == sessionID {
					line += "〔本会话〕"
				}
			}
		case callback.KindApp:
			if t.App == nil {
				line += "目标缺失"
				break
			}
			line += "快应用 " + t.App.Name + " · " + t.App.Callback
		case callback.KindScript:
			if t.Script == nil {
				line += "目标缺失"
				break
			}
			line += "脚本 " + briefSeg(t.Script.Command, 120)
		}
		if t.Enabled {
			line += " · 启用"
		} else {
			line += " · 停用"
		}
		if t.LastStatus != "" {
			line += " · 上次" + callback.StatusLabel(t.LastStatus)
		}
		if t.NextRunAt > 0 {
			line += " · 下次 " + time.UnixMilli(t.NextRunAt).Format("2006-01-02 15:04")
		}
		b.WriteString(line + "）")
	}
	b.WriteString("\n</callbacks>")
	return b.String()
}

/* modelsSection 模型清单段：task 与 model_call 的 model 参数取值唯一归属。无模型返回空。 */
func modelsSection(mc domain.ModelsConfig) string {
	ms := modelStatusList(mc)
	if len(ms) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<models>\n（可用模型清单：task 与 model_call 的 model 参数取值以此为准，不要臆造；" +
		"当前主模型每轮见 <agent_status>，增删与主模型切换见 <resource_change>）")
	for _, m := range ms {
		b.WriteString("\n- " + m.Name + "（" + m.Desc + "）")
	}
	b.WriteString("\n</models>")
	return b.String()
}

/*
mcpSection MCP 清单段：只列服务与入口。调用纪律唯一归属在 <action>
（策略），页面/脚本直调的用法唯一归属在 open-api 技能（机制）。
无服务返回空。
*/
func mcpSection(fsys osfs.OS) string {
	lines := mcpListLines(fsys)
	if len(lines) == 0 {
		return ""
	}
	return "<mcp>\n（MCP 服务清单，以此为准，不要读取 mcp.json 来发现；经 mcp_router 调用——纪律见 <action>；" +
		"页面/脚本直调走 open-api 技能；轮内变更见 <resource_change>）\n" +
		strings.Join(lines, "\n") + "\n</mcp>"
}

/*
localAPISection 本机 API 段：只保留动态事实（基址，端口来自配置）与
入口指针；端点/鉴权/模板/排错唯一归属在 open-api 技能。
*/
func localAPISection() string {
	s := "<local_api>\n（core 本机 OpenAPI：脚本/快应用/外部程序直调大模型与 MCP 工具的通道。" +
		"写调用代码前先 load_skill 加载 open-api 技能拿端点、鉴权与示例；" +
		"令牌不进对话——用户要贴就指引去设置页重取。）"
	if base := localAPIBase(); base != "" {
		s += "\n基址：" + base + "/api/open/<路径>"
	}
	return s + "\n</local_api>"
}

/*
actionSection 行动准则段：工作流与选择策略的唯一归属——技能优先、
计划审批、执行通道选择、MCP 纪律、task 分身、交付连续性、重试、界面定位。
工具机制（参数、动作清单、坑位）在各自工具描述里，不在此复述。
*/
func actionSection() string {
	return "<action>\n" +
		"# 行动准则\n" +
		"1. 接任务先匹配 <skills> 清单：命中就 load_skill 加载后按其指引执行，不要绕过现成技能自己造流程；没命中再自行设计。\n" +
		"2. 多步骤或有风险的任务，先用 ask_user 提交计划（options 给候选，如 [\"执行\",\"否决\",\"修改\"]）请用户处置，获批后再执行；一步能完成的小事直接做。\n" +
		"3. 执行通道（机制细节见各工具说明）：\n" +
		"- 本机一次性命令（构建、查询、跑脚本、下载等长任务）用 terminal——阻塞到命令结束（或超时）同步拿结果；每条命令是独立进程，cd 不跨命令保留。\n" +
		"- 交互式应答、长驻程序、要用户实时看见的过程用共享终端（term_tab / term_action）；长驻程序运行中或任务收尾时把终端入口交付给用户。\n" +
		"- 检索网页、查资料、操作网页用共享浏览器（browser_tab / browser_action，真实 Chromium，用户实时共见可直接接管）；与 terminal 互补——一个管本机命令，一个管真实浏览器。\n" +
		"4. MCP：内置工具够用就不绕道；用前先 tool_list 拉取该服务的工具清单再 tool_call，工具名与参数以 schema 为准，不臆造。\n" +
		"5. 可并行、相互独立、或会产生大量中间输出的子任务，交 task 分身执行（工具集相同、过程互不干扰，结果直接回传），主对话只接结论；任务描述必须自包含（分身看不到本轮对话之外的语境）：写清目标、输入、涉及的文件绝对路径与期望的返回格式；无依赖的子任务一次并行发多个。\n" +
		"6. 交付与连续性：任务收尾把成果入口用 <$supper_url> 交付（格式见 <output>），产物放工作目录；上下文被整理后从进度档案恢复现场接着干（路径见 <session> 块），长任务到阶段性节点也可主动把进度补写进该档案。\n" +
		"7. 重试纪律：同一手段连续 2 次无可见效果立即停——换手段（如滚不动换 key 翻页）或先建可观测信号（读返回的 url/title/scrollY、截图）确认状态再动；" +
		"调试先求\"能看见状态\"，再动目标；同一子任务调用约 10 次仍无进展即停手，向用户汇报现状与已试手段；长任务每几步用一句话报进度，不让用户猜你在干嘛。\n" +
		"8. 界面定位不要用物理像素坐标：截图、窗口位置与缩放会随 DPI、多显示器和窗口移动漂移，模型从截图估算坐标也远不准确，靠坐标点击极易点偏或直接报错。能命令/脚本完成的就不动界面；需要适配缩放时用 DPI/视口信息算尺寸可以，但界面操作一律走 DOM、选择器或键盘语义；截图只用来确认状态和布局，不用来推算点击位置。\n" +
		"</action>"
}

/*
outputSection 输出规范段：可点击入口格式、工具参数引用、回复风格——
"给用户的东西长什么样"的唯一归属（开篇与 <action> 不复述风格要求）。
*/
func outputSection() string {
	return "<output>\n" +
		"# 输出规范\n" +
		"## 可点击入口 <$supper_url>（格式错就不会渲染成可点击入口，用户只能看到原文，务必照抄格式）\n" +
		"规则：整段闭合包裹、标签紧贴地址、地址内不能有空格/换行/文字；说明文字写在标签外面。\n" +
		"正确示例：\n" +
		"<$supper_url>https://example.com/report</$supper_url>\n" +
		"<$supper_url>term://终端id</$supper_url>（id 来自 term_tab）\n" +
		"<$supper_url>browser://标签id</$supper_url>（id 来自 browser_tab）\n" +
		"<$supper_url>app://快应用名</$supper_url>\n" +
		"<$supper_url>file://C:/完整/绝对/路径.md</$supper_url>\n" +
		"错误示例（不会渲染）：\n" +
		"<$supper_url>点这里看终端 term://t1</$supper_url>   ← 地址里夹了文字\n" +
		"<$supper_url>term:t_1</$supper_url>                 ← 缺 //\n" +
		"<$supper_url>https://example.com                    ← 没闭合\n" +
		"## 工具参数引用 <@toolArg>\n" +
		"工具参数的字符串值里写 <@toolArg>绝对路径</@toolArg>，执行时自动展开为该文件内容——" +
		"需要把已生成的文件全文作为参数喂给工具（如 save_app 的代码参数引用草稿文件）时用它，" +
		"省去先 read_file 再粘贴；单文件上限 200000 字符，读不到会报错。\n" +
		"## 回复风格\n" +
		"结论先行、简洁：先给结果与入口，再给必要说明；不逐条复述工具输出；" +
		"用户在终端/浏览器里看得见的过程不要文字直播；长说明用列表。\n" +
		"</output>"
}

/* localAPIBase 返回本机 API 基址（供 <local_api> 段告知完整地址；读不到配置返回空）。 */
func localAPIBase() string {
	cfg, err := config.Load()
	if err != nil || cfg.Port <= 0 {
		return ""
	}
	return "http://127.0.0.1:" + strconv.Itoa(cfg.Port)
}
