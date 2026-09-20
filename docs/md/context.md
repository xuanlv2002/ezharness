# 上下文：模型看到的消息序列（逐条标注）

模型看到的 = **system（每 session 固定）+ 历史（与磁盘同源）+ 每轮按需插入的记录**。
本文给出一份**完整上下文**并逐条标注来源，回答两个问题：system 里都是谁写的哪一段；一轮用户输入前后有哪些标签、谁插的、什么条件才插。

runtime.md 讲流程（什么时候发生），本文讲**成品长什么样**。改任何一段文案、标签或插入位置，先在这里对一遍——前端历史重建依赖标签识别，标签与插入条件是硬契约。

## 一、system 的组成（按拼接序）

`sysprompt` 是 system 消息的唯一来源（先有 system，后续 hook 才能往它追加）；后续每个 hook 的 `OnStart` 依次往 `Messages[0]` 追加自己的段。**拼接序 = startHooks 装配序**（`core/internal/service/agent_service.go` 的 `WithHooks` 列表）。

| 序 | 段 | 生成者 | 内容 | 条件 / 刷新 |
|---|---|---|---|---|
| 1 | 人设段 | `buildSystemBase`（`service/agent_service.go`） | 「你是 ezharness——…」+ 总纲（接任务看 `<action>`、格式守 `<output>`）+ 设置页 `SystemExtra` | session 创建时组装一次 |
| 2 | `<workspace>` | 同上 | 三个可写位置（workspace / longterm / skills，全绝对路径）+ 行为规则（资源发现以段为准、不翻存档、不碰配置） | 同上 |
| 3 | `<memory>` | 同上 | 记忆结构说明（固定主题文件 user/projects/lessons）+ `harness.md` 全文（`EnsureHarnessMd`） | 同上 |
| 4 | `<skills>` | 同上 | 技能清单（`skill.LoadDir`，按 `DisabledSkills` 过滤） | 有技能才有 |
| 5 | `<mcp>` | 同上 | 启用的 MCP server 清单 | 有 server 才有 |
| 5a | `<action>` | 同上 | 行动准则：技能匹配 → 计划审批 → 工具选择（terminal/term_*/browser_*）→ MCP 发现 → task 分身 → 交付与连续性（progress.md） | 同上 |
| 5b | `<output>` | 同上 | 输出规范：`<$supper_url>` 正误示例（格式错不渲染）、`<@toolArg>` 用法、回复风格 | 同上 |
| 6 | `<session>` 身份块 | `SysPrompt.identityFn`（`hooks/sysprompt.go` `SessionIdentityBlock`） | 会话 ID + 存档与进度档案绝对路径（trim 折叠后的回忆入口 + 恢复现场） | **每次 render 实时求值**（换代后 ID 自动正确） |
| 7 | `<compact-summary>` | `hooks/archive.go` 归档时写入 summary 段 | 「上一会话已归档…已沉淀进 memory/longterm/…；本会话开始前的交接摘要：…」 | 仅 compact 换代后的 session |
| 8 | `# 系统环境` | ezloop `filetools` `OnStart`（`injectOSHint`） | GOOS + 对应 shell 语法提示 | 每轮一致（GOOS 进程内恒定） |
| 9 | `<tool-guide>` ×6 | 按序：`skilltool` → `approve` → `askuser` → `task` → `mcp` → `trim` | 每个工具一段行为约定（何时用、边界在哪） | 恒有 |

三个要点：

- **1–5 只组装一次**：base 存 `Session.sysP` 并落 `session.json` 快照（`sessionstore`），重启从快照还原、不重组。所以**改了记忆/skill/MCP，下个 session 才进 system**；期间由轮首 `<resource_change>` 告知模型（这正是 remind 变更段存在的理由之一）。
- **1–7 合成 system，8–9 追加在它末尾**：整条 system 每轮由 `sysprompt.OnStart` 重写（`base + identity + summary`），追加段随各自 hook 的 `OnStart` 每轮重新拼上。
- **fork 不吃这一套**：startHooks 不重跑，分身 seed 已含完整 system（`core/fork.go`）。

## 二、一份完整上下文（逐条标注）

下面是 compact 换代后、带文件引用且读图的一轮（历史省略）。左侧标注对应第三节表格。

```
[system]
你是 ezharness——一个持续陪伴用户的设备级 agent，可全权操作本机文件与命令。…   ← ①
<workspace>
# 工作区：三个可写位置（下列均为完整绝对路径，直接使用，不要自行拼接）
# …/data/workspace         工作目录，草稿/脚本/命令产物一律放这里；tmp/ 附件暂存（read_file 读，图片自动进上下文）
# …/data/memory/longterm   长期记忆：harness.md 是索引（已注入，见 <memory>），user/projects/lessons 固定主题文件
# …/data/memory/skills     技能库：每技能一子目录，新建后下个 session 进清单
# 行为规则（不需要记路径）：技能/MCP 清单以 <skills>/<mcp> 段为准不读目录发现；快应用由 save_app 生成；
# 存档与进度档案见 <session> 块，历史会话不主动翻阅；超长工具结果按提示 read_file 取回；配置文件不改写；
# terminal 每条命令独立进程；一律绝对路径。
</workspace>                                                              ← ②
<memory>
# 长期记忆
- 索引 …/memory/longterm/harness.md：长期记忆入口，全文见下方…
- 主题文件 …/longterm/{user,projects,lessons}.md：偏好事实/项目背景/踩坑经验，不进上下文，归档时自动合并
# 索引 harness.md 全文
（harness.md 全文）
</memory>                                                                ← ③
<skills>
（可用技能清单，以此为准，不要读取 memory/skills 目录来发现技能；技能正文在 …/memory/skills/<名>/SKILL.md…）
- 周报生成: 把本周 git 变更整理成周报
</skills>                                                                ← ④
<mcp>
（可用 MCP 服务清单，以此为准，不要读取 mcp.json 来发现服务；经 mcp_router 工具调用，先用 tool_list 拉取某服务的工具清单再 tool_call…）
- playwright: 浏览器自动化
（本机 HTTP 直调：http://127.0.0.1:5262/api/mcp/call，POST {"server","tool","args"} 返回 {"result"}；构建可观测页面等快应用时把 MCP 工具当本地接口直接 fetch）
</mcp>                                                                   ← ⑤
<action>
# 行动准则
1. 接任务先匹配技能：对照 <skills> 清单，命中就 load_skill…
2. 动手前想清楚：多步/有风险任务先 ask_user 提交计划（options 候选）获批再动手…
3. 执行工具的选择：一次性命令 terminal / 交互长驻共见 term_* / 网页检索 browser_*…
4. MCP：内置够用不绕道；先 mcp_list/tool_list 发现再 tool_call…
5. 独立或大中间输出子任务交 task 分身，描述自包含，无依赖并行发…
6. 交付与连续性：成果入口 <$supper_url> 交付；整理后从进度档案恢复现场…
</action>                                                                ← ⑤a
<output>
# 输出规范
## 可点击入口 <$supper_url>（格式错就不会渲染成可点击入口…）
正确示例：<$supper_url>term://终端id</$supper_url> …
错误示例（不会渲染）：<$supper_url>点这里看终端 term://t1</$supper_url> …
## 工具参数引用 <@toolArg> …
## 回复风格：结论先行…
</output>                                                                ← ⑤b
<session>
当前会话 ID：20260915-234850-db29e69f
本会话存档：…/sessions/20260915-234850-db29e69f/session.json
（这是本会话的完整历史档案：上下文整理折叠掉的早期对话仍完整保留在此文件中…）
进度档案：…/sessions/20260915-234850-db29e69f/progress.md
（每次上下文整理自动重写：已完成/正在做/待办/关键事实。恢复任务现场或阶段性节点先读写它…）
</session>                                                               ← ⑥
<compact-summary>
上一会话已归档：原始记录在 …/sessions/20260915-233831-6c845611/session.json（可读取全文）；
值得长期保留的偏好/事实/经验已沉淀进 memory/longterm/（user/projects/lessons，索引见 harness.md），需要时检索。
本会话开始前的交接摘要：
（上一世代摘要正文）
</compact-summary>                                                       ← ⑦
# 系统环境
当前操作系统：windows（terminal 工具用系统原生 shell 执行，Windows 请写 cmd 语法 dir/type/findstr…）  ← ⑧
<tool-guide>
load_skill：接任务先扫技能清单，命中就加载后按其指引执行，不要绕过现成技能自己造流程。
</tool-guide>                                                            ← ⑨a skilltool
<tool-guide>
审批：写操作等有副作用的工具调用会先请用户批准再执行；被拒绝时理由会作为工具结果返回…
</tool-guide>                                                            ← ⑨b approve
<tool-guide>
ask_user：缺少必要信息、需要澄清或确认方向时向用户提问，不要替用户假设。…
</tool-guide>                                                            ← ⑨c askuser
<tool-guide>
task：可并行或较复杂的子任务交给 task 分身隔离执行（工具集相同、过程互不干扰…）。
</tool-guide>                                                            ← ⑨d task
<tool-guide>
mcp_router：访问外部能力（已配置的 MCP server）统一入口，先 mcp_list / tool_list 发现可用能力…
</tool-guide>                                                            ← ⑨e mcp
<tool-guide>
trim_context：把早期对话就地折叠为摘要，上下文立即变小（会话与历史档案不变）。…
</tool-guide>                                                            ← ⑨f trim

[user]
<context_trim kept="12">
（系统自动整理，非用户发言，无需回应）
此前的早期对话已折叠出模型上下文，整理结果已写入进度档案 sessions/…/progress.md（路径见 <session> 块），原始记录仍完整保留在会话存档。当前任务衔接摘要：
【已完成】
- （折叠段：已完成事项与关键结果）
【正在做】
- （当前进行中事项与进行到哪一步）
【待办】
- （已明确但未完成的事项）
【关键事实】
- （用户要求与偏好、已定决策、路径/ID 标识）
</context_trim>                                                          ← ⑩ 本 session 早前整理过才有
[user]     （历史……）
[assistant]（历史……）

[user]
<resource_change>
（系统检测到的本轮资源变更，非用户发言，无需回应，无需回溯处理）
- 新增技能 周报生成
变更后完整清单（技能与 MCP 服务以此为准，不要读取 memory/skills 目录或 mcp.json 来发现技能与服务）：
available_skill: 周报生成 - 把本周 git 变更整理成周报
available_mcp: playwright - 浏览器自动化
</resource_change>                                                       ← ⑪ 有资源变更才插（available_ 清单行给模型，变更卡不展示）
[user]
<agent_status>
当前时间：2026-09-15 23:49
上下文水位：26663 / 128000 tokens
距上次输出：3 分钟
当前运行中终端：编译监控（盯构建）[t1]、爬虫[t2]
当前开启浏览器标签：文档（React 文档）[b3]
</agent_status>                                                          ← ⑫ 每轮都插
[user]
<reference_file>
{"hint":"用户本轮引用了以下本地文件，可用 read_file 读取","refs":[{"path":"…/tmp/att-20260915-234900-1-报错截图.png"},{"path":"…/src/main.ts","items":[{"sel":"timeout: 300","note":"这里太短了","from":118,"to":131}]}]}
</reference_file>                                                        ← ⑬ 有引用才插
[user]
看下这两个文件，帮我定位问题                                                ← ⑭ 本轮 input（引擎先入史，⑪⑫⑬ 插在它之前）

[assistant] [tool_use read_file call_00_a]  {"path":"…/att-…-报错截图.png"}
            [tool_use terminal  call_00_b]  {"command":"findstr /n timeout src\\main.ts"}   ← ⑮ 模型
[tool]     <image_loaded path="…/att-20260915-234900-1-报错截图.png"/>      ← ⑯ read_file 读图的机器标记（原文保留不改写）
[tool]     Microsoft Windows [版本 10.0.26200.8875] …                      ← ⑰ terminal 结果
[user]
<image_loaded>
…/att-20260915-234900-1-报错截图.png
</image_loaded>                                                          ← ⑱ 本条 Images 带 base64（filetools OnLoop 批末插入）
[assistant] 定位到了：main.ts 里的 timeout 只有 300ms，慢接口必然超时。…      ← ⑲ 模型
[user]
<end_reason>
（系统自动记录的轮次收尾信息，非用户发言，无需回应）
运行轮次：2
运行时长：1 分 12 秒
结束时间：2026-09-15 23:50:12
结束原因：正常结束
</end_reason>                                                            ← ⑳ 每轮末
```

## 三、一条用户输入前后的时序带

把第二节压缩成时间顺序（这就是"哪些标签、什么条件"的答案）：

```
        上一轮末  <end_reason>        每轮都有，OnEnd 尾插
   ┌── 轮首（startHooks，全部插在本轮 input 之前，按 hook 序）
   │    <resource_change>  仅当 skill/mcp 基线有变更（附变更后 available 完整清单）
   │    <agent_status>     每轮都有（水位/时间/距上次输出/运行中终端/浏览器标签）
   │    <reference_file>   仅当本轮带附件或文件引用
   ├── 本轮 input           引擎 AppendMessage（先入史，上面三条插到它前面）
   ├── 迭代（≤MaxIterations）
   │      assistant（正文 / tool_use）
   │      tool（结果；读图带 <image_loaded path="…"/> 机器标记）
   │      → 批末：<image_loaded> 图片消息           仅当本批读了图（OnLoop）
   │      → 回边：文件变更/水位整理/热加载（OnLoop）
   └── 轮末（endHooks）
          <end_reason>      每轮都有，OnEnd 尾插

   <context_trim>           整理发生时尾插（同 session 内就地折叠，位置由折叠点决定）
```

## 四、逐条对照表

| 标注 | 消息 | 生成者 | 插入点 | 条件 | 前端（实时 / 历史） |
|---|---|---|---|---|---|
| ⑪ | `<resource_change>` | `hooks/remind.go` `OnStart`（变更段，diff 在 `reschange.go`） | 轮首，input 前 | skill/mcp 基线有变更 | `resource.change` 事件 / 标签 + `- ` 行（available_ 行只给模型） |
| ⑫ | `<agent_status>` | 同上（快照段，`renderStatus`） | 轮首，input 前 | 每轮 | `status.snapshot` 事件（右上角水位条）/ 含关键词「整理上下文」才入时间线 |
| ⑬ | `<reference_file>` | `hooks/reference_file.go` `OnStart` | 轮首，input 前 | 有附件或文件引用 | 发送时本地 push user 块 / 解析 JSON 载荷出 files·fileRefs chips |
| ⑭ | 用户输入 | 引擎 | 每轮 | — | 发送时本地 push / 按角色渲染 |
| ⑯ | `<image_loaded path="…"/>` 自闭合 | `read_file` 工具结果（ezloop filetools） | 工具批内 | 读到图片 | 工具卡结果文本 / 同左（不解析） |
| ⑱ | `<image_loaded>` 包裹标签 | ezloop `filetools` `OnLoop` | 批末（最后一条 tool 之后） | 本批有读图 | `filetools.image_loaded` 事件 / 标签体内路径行 + `m.images` base64 |
| ⑳ | `<end_reason>` | `hooks/remind.go` `OnEnd`（收尾段） | 轮末 | 每轮 | `turn_end` 合成 endtick / 正则提取字段 |
| ⑩ | `<context_trim kept="N">` | `hooks/trim.go` `doTrim` | 整理时尾插（序列尾部，时间序自然） | 水位自动（OnLoop）或模型调 `trim_context` | `session.trim` note / 标签 → note 分割线（`trimText` 按【已完成】锚点截 160 字，旧"摘要："格式不兼容）；同内容重写 `sessions/<id>/progress.md` |

两条"模型自己写、系统只负责解析"的语法（指引都在 `<workspace>` 段，不在此表）：`<$supper_url>类型://标识</$supper_url>` 是回复里的可点击入口；`<@toolArg>绝对路径</@toolArg>` 是工具参数的语法糖，执行时由 `toolarg` warp 展开、入史保留原文。

## 相关文档

- **runtime.md** — 一轮 chat 的完整流程与 hook 清单（本文是它的"成品视图"）
- **frontend.md** — 这些标签在前端怎么被解析与渲染
- **session.md** — 历史如何落盘与恢复（system/摘要段的快照字段）
