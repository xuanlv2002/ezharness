# AGENTS.md — ezharness 开发文档

给 AI 的开发参考：项目架构与文件职责、hook 与上下文来源、warp 作用。
改代码前先定位到对应小节。

---

## 一、架构与文件职责

### 1.1 三层

| 层 | 目录 | 职责 |
|---|---|---|
| 前端 | `frontend/` | Svelte 5（runes）单页应用，构建产物 `dist/` |
| 后端 | `core/` | Go sidecar（module `ezharness/core`，产物 `ezharness-core.exe`），gin 服务 + agent 装配；业务全在这里 |
| 壳 | `desktop/` | Electron 主进程（纯 CommonJS，无构建步骤）：拉 core、开窗、托盘、桌面资产（共享浏览器）。不做业务 |

前端产物构建时拷到 `core/web/dist`，由 `core/embed.go` 的 `go:embed` 打进二进制（目录缺失则 go build 直接失败）。
桌面形态由 `desktop/` 拉起 core；core 直接运行则是 web 端形态（浏览器等桌面能力自动降级）。
应用根 = core exe 所在目录（`core/internal/config/cfg.go`，`--root` 可覆盖），配置与数据（`ezharness.json`、`sessions/`、`memory/`…）都在那里，进程启动时 chdir 到数据目录。

### 1.2 core 分层与文件

```
controller（gin handler） → service（用例） → domain（会话聚合 + 事件）
tools / hooks 为领域扩展；osfs / config 为基础设施；warp 包模型与工具；main / app 只做装配
```

| 路径 | 职责 |
|---|---|
| `core/main.go` | 入口：解析 `--root`、`config.Load`、`newApp`、`buildRouter` 装配整栈 |
| `core/app.go` | 应用生命周期：持有当前代 `http.Server`；**重启 = 换代**（关旧 server → 切数据目录 → 重建 Hub/Router → 新 server），`boot` 代际计数供前端判断就绪 |
| `core/embed.go` | `go:embed web/dist` + `distFS()` |
| `core/internal/config/cfg.go` | 应用根、端口、数据目录、chdir |
| `core/internal/controller/router.go` | 全部路由表 + SPA 静态兜底（`/api` 之外回退 index.html） |
| `core/internal/controller/*.go` | 表现层，只绑定/校验/响应：`app` `session` `chat` `topic` `settings` `mcp` `apps` `terminal` `browser` `workspace` |
| `core/internal/service/agent_service.go` | **最重要的文件**：`Assemble` 装配一个 session 的 agent——hook 列表、warp 链、工具集、ToolNames、system 基础段全在这里 |
| `core/internal/service/chat_service.go` | 一轮运行的启停、SSE 推送、用量统计 |
| `core/internal/service/session_service.go` | 历史读取、bootstrap、status/水位 |
| `core/internal/service/topic_service.go` | 分支线（topics.json）、归档换代、分叉 |
| `core/internal/service/settings_service.go` | 设置/模型/安全规则/记忆/skill 读写 |
| `core/internal/service/terminal.go` | 共享终端（PTY 会话池 + WS 多路复用 + readMark 游标） |
| `core/internal/service/browser.go` | 共享浏览器 core 侧：经 `/api/browser/bridge` 把 `browser_*` 转发给 desktop |
| `core/internal/service/mcp.go` `stash.go` `apps_service.go` `app_service.go` | MCP 客户端与热重载 / 附件暂存 / 快应用 / 换代重启 |
| `core/internal/domain/session.go` | `Hub`（全局根：会话表、Topics、Fsys、设置快照）+ `Session` 聚合（history、`StartRun`/`FinishRun`/`Cancel`、`Publish`、`replayable` 名单） |
| `core/internal/domain/event.go` | SSE 事件类型与 `MapEvent` |
| `core/internal/domain/settings.go` `toolrules.go` `stats.go` | 设置模型 / 工具审批规则 / 用量统计 |
| `core/internal/hooks/` | 宿主侧 hook 与相关实现，见第二节 |
| `core/internal/tools/` | 宿主侧工具：`tools.go`(save_app)、`term.go`(term_*)、`browser.go`(browser_*)、`vision.go`(image_recognize)；接口定义在 tools、实现在 service（避免 import 环） |
| `core/internal/warp/` | 模型/工具装饰器，见第三节 |
| `core/internal/osfs/osfs.go` | 无沙箱全权限 FileSystem |

### 1.3 frontend

| 路径 | 职责 |
|---|---|
| `index.html` `src/main.ts` | 唯一入口（无第二页面） |
| `src/App.svelte` | 主壳：视图切换、工作区抽屉挂载、`?popout=<tool>` 单工具弹窗模式（不 bootstrap，只渲染一个 pane） |
| `src/lib/api.ts` | REST + SSE 客户端与全部载荷类型 |
| `src/lib/store.svelte.ts` | 全局状态机：bootstrap、SSE 归约（`apply`）、历史重建（`buildBlocks`）、抽屉/弹窗/草稿/附件/通知。前端最核心的文件 |
| `src/lib/term.ts` | 终端 WS 管理器：单连接多路复用（帧带 id 路由）+ 断线重连 + hello 快照 |
| `src/lib/desktop.ts` `textfile.ts` `filePaneState.ts` | `?desktop=1` 判定 / 路径工具 / 资源页跨窗口状态胶囊 |
| `src/components/ChatView.svelte` `Timeline.svelte` `MessageItem.svelte` `InputBar.svelte` | 对话主列：时间线按 block kind 分发、消息渲染、输入框与附件 |
| `src/components/ToolBlock.svelte` `ToolGroup.svelte` `StatusCard.svelte` `StatusTagCard.svelte` `ResChangeCard.svelte` `DecisionCard.svelte` `NoticePanel.svelte` `ForkCard.svelte` `ForkPanel.svelte` `BranchPanel.svelte` | 时间线卡片族与分支/分身面板 |
| `src/components/Sidebar.svelte` `TitleBar.svelte` `ModelsView.svelte` `MemoryView.svelte` `KnowledgeView.svelte` `ToolsView.svelte` `McpView.svelte` `SecurityView.svelte` `SettingsView.svelte` | 侧栏、标题栏与各设置页 |
| `src/components/board/` | 工作区抽屉：`WorkspaceDrawer.svelte`（容器 + 拖拽脱离手势）、`TerminalTab.svelte`（xterm per 终端保活）、`BrowserPane.svelte`（标签条/地址栏 + 内容区 rect 上报） |
| `src/components/viewer/` | 资源查看器：`ResourcePane.svelte`（多 tab 框架）、`registry.ts`（扩展名 → viewer 的单一路由表）、`viewers/*`（text/markdown/html/image/pdf/fallback，图片走 `MagicBoard` 画板） |

### 1.4 desktop

| 路径 | 职责 |
|---|---|
| `desktop/src/main/index.js` | 主进程：spawn core + 健康轮询、无边框主窗、托盘、关闭语义（托盘/确认框）、快应用子窗、抽屉工具的弹出窗口与**拖拽脱离**（tear-off）、全部窗口类 IPC |
| `desktop/src/main/browser/index.js` | 共享浏览器 = **WebContentsView**（每标签一个，`partition: persist:ezbrowser` 共享登录态）；抽屉页只做 UI，内容区 rect 由渲染层上报、主进程 `setBounds` 贴靠；同时是 core `/api/browser/bridge` 的 WS 客户端与执行器 |
| `desktop/src/preload/index.js` | contextBridge 暴露 `window.ez`（`window.*` / `popout.*` / `browser.*`）；web 端没有它，所有调用点判空降级 |

### 1.5 其他

`script/dev.bat`（前端 build → 拷 `core/web/dist` → go build 出 `bin/ezharness-core.exe` → electron 跑起来）、`script/release.bat`（+ electron-builder）、`script/version.yaml`（版本单源）、`docs/md/*`（设计文档）、`docs/pending.md`（待办）。

---

## 二、hook 与上下文

### 2.1 hook 是什么 / 在哪注册

hook 是 ezloop 引擎的横向扩展点（接口见 ezloop `hook/hook.go`：`OnStart` / `OnModelStart` / `OnModelEnd` / `OnToolStart`→`Action` / `OnToolEnd` / `OnLoop` / `OnEnd`，按需实现接口即可）。
**注册处唯一**：`core/internal/service/agent_service.go` 的 `Assemble`（`core.WithHooks(...)`，约 184–200 行）。每个会话/分支装配一次，`Reassemble` 时重建。

注册顺序即执行顺序，且顺序有语义：

- `OnStart` 按序跑，各 hook 往 `Messages[0]` 追加自己的说明段 → **`sysprompt` 必须首位**（system 消息的创建者）
- `OnToolStart` 按序跑，**首个返回 Skip 的会短路后续 toolStart hook** → `approve` 排在 `task`/`offload`/`trim` 之前，拒绝的调用到不了它们
- `guard` 必须在 `offload` 之后（要看已被 offload 改写过的结果）
- `remind` 在 `sessionstore` 之前（`<end_reason>` 要进落盘快照）
- `sessionstore` 最后（落盘）

当前链序：
`sysprompt → contextfix → filetools → skilltool → remind → reference_file → approve → askuser → task → mcp → offload → guard → trim → trace → sessionstore`

### 2.2 宿主 hook（`core/internal/hooks/`）

| 文件 | 接口方法 | 职责 |
|---|---|---|
| `sysprompt.go` `SysPrompt` | `OnStart` | system 消息唯一来源；渲染 = base + 身份块 + 摘要块 |
| `remind.go` `Remind` | `OnStart` `OnEnd` | 轮首注入 `<agent_status>` 快照（时间/水位/距上次输出），有资源变更时另插 `<res_change>`；轮末注入 `<end_reason>` |
| `reschange.go` | （`Remind` 的内部实现） | skill/MCP/终端基线 diff → 变更条目 |
| `reference_file.go` `RefFileHook` | `OnStart` | 本轮有附件/文件引用时，在用户输入前插 `<reference_file>` 结构化消息 |
| `trim.go` `Trim` | `OnStart` `OnLoop` `OnToolStart` | 注册 `trim_context` 工具；回边水位整理：就地折叠早期消息 + 插 `<context_trim>` 标记（同一 session 内） |
| `guard.go` `Guard` | `OnToolEnd` | 窗口余量兜底：offload 豁免名单（read_file 等）的大结果放不下时也卸载 |
| `sessionstore.go` `Store` | `OnStart` `OnEnd` | 轮末把历史与 system 快照写 `sessions/<id>/session.json` |
| `trace.go` `Trace` | 全部 | 跨度记录 → `trace.jsonl` |
| `archive.go` `ArchiveSession`（函数，非 hook） | — | 归档换代：总结 → 封旧 session → 开新代；由 `service/topic_service.go` 调用 |
| `summarize.go`（函数） | — | trim 与 archive 共用的总结器 |
| `topics.go` `Topics` | — | `topics.json` 分支线索引管理（非 hook） |
| `memory.go` `Memory`、`recall.go` `Recall` | — | **已定义但未接线**（无 `NewMemory`/`NewRecall` 调用点）。长期记忆实际由 `buildSystemBase` 的 `<memory>` 段注入；`recall_topic` 未注册 |

同时接入的 ezloop hook：`approve`（人审）、`askuser`（`ask_user` 工具）、`contextfix`（修补孤立 tool 消息对）、`filetools`（read_file/write_file/edit_file/terminal）、`skilltool`（`load_skill`）、`task`（分身）、`offload`（大结果卸载，`WithSkip(ask_user, task, load_skill)` + `WithReplayTool("read_file")`）、`mcp`（`service/mcp.go` 的 `NewMcpHook` 包装，带热重载闭包）。

禁用名单等实时配置以**闭包**注入（`disabledSkills`、`mainVision` 等），因为 `hooks` 被 `domain`/`service` 依赖，不能反向 import。

### 2.3 上下文都是哪来的

**system 段** — `agent_service.go` 的 `buildSystemBase`（约 431 行）。段序：人格 → `SystemExtra`（设置页）→ `<workspace>`（目录架构/权限、`<@toolArg>` 与 `<$supper_url>` 语法、终端与浏览器工具指南）→ `<memory>`（含 `harness.md` 全文，`hooks.EnsureHarnessMd`）→ `<skills>`（`skill.LoadDir`，按 `DisabledSkills` 过滤）→ `<mcp>`。
每 session **只组装一次**：存 `Session.sysP`（`domain/session.go` 的 `SetSysP`/`SysPromptRef`）并落进 `session.json` 快照（`sessionstore.go`）；改了记忆/skill/MCP 要下个 session 才进 system，期间由 `<res_change>` 告知模型。归档换代时热换。

**历史** — `Session.history`（`domain/session.go`），落盘 `sessions/<id>/session.json`。`StartRun` 把 `modelViewLocked(history)` 交给引擎；ModelView 从**最后一个 `<context_trim>` 标记**起（折叠掉的旧档只留在存档里）。`FinishRun`：本轮有折叠 → `hooks.MergeFull(history, state)`，否则整轮替换。`Store.OnEnd` 对磁盘快照做同样的合并。
**trim** = 同一 session 内就地折叠（`hooks/trim.go`）；**compact/归档** = 换代新 session + 摘要（`hooks/archive.go`）——这两件事的边界别混。

**每轮注入的标记记录**（都是 `role=user` 的消息，随轮末落盘；前端从历史重建时间线，所以落盘与否很关键）：

| 标签 | 生成处 | 落盘 | 前端消费 |
|---|---|---|---|
| `<agent_status>` | `hooks/remind.go` `renderStatus` | 是 | `store.svelte.ts` `parseStatus` / `buildBlocks` |
| `<res_change>` | `hooks/reschange.go` + `remind.go` `renderResChange` | 是 | `buildBlocks` + `apply('res.change')` + `ResChangeCard.svelte` |
| `<reference_file>` | `hooks/reference_file.go` | 是 | `buildBlocks` |
| `<end_reason>` | `hooks/remind.go` `OnEnd` | 是 | `buildBlocks` / `endReasonText` |
| `<context_trim>` | `hooks/trim.go` `doTrim` | 是 | `buildBlocks` / `trimText` |
| `<image_loaded>` | 工具产出标记（`filetools` 的 read_file、`service/browser.go` 截图）→ `filetools` `OnLoop` 转成图片消息 | 是 | `buildBlocks` → `Timeline.svelte` imgload 行 |
| `<$supper_url>` | 模型自己在回复里写（约定在 `<workspace>` 段） | 是 | `MessageItem.svelte`（渲染可点 chip，回跳抽屉/终端/浏览器/快应用） |
| `<@toolArg>` | 模型写，执行时由工具 warp 展开（见第三节） | 原文保留 | 无 |

实时侧对应 SSE 事件（`domain/event.go` 的 `MapEvent` → 前端 `store.apply`）：部分事件在 `session.go` 的 `replayable` 名单里被排除（如 `turn_end`、`res.change`、`status.snapshot`），断线重连靠历史重建，不靠回放。

### 2.4 常见改动落点

- **加/删/换一个 hook** → `agent_service.go` 的 `Assemble`，`core.WithHooks(...)` 列表；注意上面 2.1 的顺序规则
- **改 system 文案与块结构** → `buildSystemBase`（`agent_service.go`）。已在跑的 session 不会回填（system 固定），只对新 session 生效
- **改提醒（`<agent_status>` / `<res_change>` / `<end_reason>`）文案** → `hooks/remind.go` + `hooks/reschange.go`；同时检查前端解析：`store.svelte.ts` 的 `buildBlocks` 按关键词判定（如 `整理上下文`），`StatusTagCard.svelte` 按文案解析，改文案必须同步，否则记录被吞或全量铺开
- **改 trim / 归档语义** → `hooks/trim.go`（同 session 折叠）与 `hooks/archive.go`（换代），两者共用 `hooks/summarize.go`；折叠边界由 `hooks.ViewStart` / `MergeFull` 定义
- **加一个新标记标签** → 生成处 + 落盘（决定前端刷新后能否重建）+ `buildBlocks` 分支 + 消费组件，四处配套

---

## 三、warp

### 3.1 是什么

warp 是 ezloop 的**纵向**装饰器（与横向的 hook 并列），包住单个节点：

- **model warp**：`Handler[provider.ModelProvider]`，包 provider，用于重试/降级/请求改写
- **tool warp**：`Handler[types.Tool]`，包工具，用于参数改写/并发限流/panic 兜底。经 `ToolRegistry.SetWarp` 生效，**静态工具与 hook 注册的工具都覆盖**

`warp.Chain` 反向迭代 handler，所以**先注册的在外层**，调用顺序 = 注册顺序 → 节点。

### 3.2 装配位置与链序

唯一装配点：`core/internal/service/agent_service.go` 的 `Assemble`——
模型链 `core.WithModelWarp(...)`（约 161 行），工具链 `core.WithToolWarp(...)`（约 182 行）。

**模型链**（先 = 外）：
`modeldump → modelretry → noempty → visionguard → provider`

| warp | 来源 | 作用 |
|---|---|---|
| `modeldump` | 本仓库 `internal/warp/modeldump` | 调试打印每次请求（Messages + Tools，图片 base64 截断）。放最外层，重试不会重复打印 |
| `modelretry` | ezloop `ext/warp/model/modelretry` | 指数退避重试 |
| `noempty` | 本仓库 `internal/warp/noempty` | 纯附件轮：user 消息 content 为空时填占位文案（否则部分 provider 报错） |
| `visionguard` | 本仓库 `internal/warp/visionguard` | 主模型 `Vision=false` 时剥掉请求里的图片、给 `<image_loaded>` 开标签加 `omitted` 说明。**只改请求副本，落盘历史不动**（换回多模态自动恢复） |

**工具链**（先 = 外）：
`toolarg → limit → safetool → tool`

| warp | 来源 | 作用 |
|---|---|---|
| `toolarg` | 本仓库 `internal/warp/toolarg` | 递归展开参数字符串里的 `<@toolArg>路径</@toolArg>` 为文件内容；只影响执行，历史保留原文标签 |
| `limit` | ezloop `ext/warp/tool/limit` | 并发工具执行上限（当前 4） |
| `safetool` | ezloop `ext/warp/tool/safetool` | panic → error，并给工具名加前缀 |

**加一个新 warp**：在对应文件写 `func Warp(...) warp.ModelHandler` / `warp.ToolHandler`，然后插进 `agent_service.go:161` 的切片或 `:182` 的 `WithToolWarp(...)` 参数即可；工具 warp 不需要额外注册到工具上。

注意：`core/go.mod` 里指向本地 ezloop 的 `replace` 目前**是注释状态**，实际编译用的是 module cache 里的 `ezloop@v1.3.8`；要改 ezloop 本身须先启用 replace（发布前再升版本去掉）。

---

## 四、文件与资源流转

**一句话**：磁盘是唯一真身，会话里只流动**路径**。文件从不复制进某个"项目目录"，只是在各自的真身位置被读写；发送时把路径告诉模型，模型按需读。

### 4.1 目录布局

应用根 = 数据目录 = core exe 所在目录（也是进程 cwd，`config.Root()`；`--root` 可覆盖）：

| 路径 | 用途 |
|---|---|
| `workspace/` | 工作目录（`service.ResolveWorkDir`：设置里的 `WorkDir` 为空 = 数据目录下 `workspace/`；相对路径按数据目录解析）。也是 terminal / 共享终端的执行目录 |
| `<工作目录>/tmp/` | **附件暂存区**：拖入、粘贴、画板产物都落这里（`service.StashFiles`，命名 `att-<YYYYMMDD-HHMMSS>-<自增序号>-<净化文件名>`） |
| `<工作目录>/browser/` | 浏览器截图（`<tabID>-<时间戳>.png`，每次一张新文件，不覆写） |
| `apps/` | 快应用 HTML（`save_app` 写入，`/apps/*` 静态服务） |
| `memory/longterm/` | 长期记忆：`harness.md`（索引，全文进 system）+ 主题文件（按需 grep） |
| `memory/skills/<名>/` | 技能（`SKILL.md` + `scripts/`） |
| `sessions/<id>/session.json` | 会话历史（含图片消息的 base64 本体） |
| `.ezloop/offload/` | 大工具结果的卸载区（模型按需读回） |
| `settings.json` `models.json` `mcp.json` `topics.json` `stats.json` `toolRules.json` | 应用配置与索引 |

### 4.2 四条典型流转

**① 把文件拖入 / 粘贴 / 复制到输入框**
`ChatView.addFiles` → `api.stash`（multipart）→ `POST /api/workspace/stash` → 落盘 `workspace/tmp/` → 返回绝对路径 → 输入框 chip（`Attachment{name, path}`）。
点击 chip = `store.openFileAt(path)`，直接在资源页打开那个 tmp 真身。**原文件不动、也不复制**。

**② 画板画一张图**
画笔钮 → `store.openImageDraft()`：空白则前端生成 960×600 白底 PNG，续编则用 chip/消息里的图 → 一样 `stash` 到 `workspace/tmp/` → 按普通图片资源打开。
资源页 `ImageViewer`（MagicBoard 画布）的改动**防抖 400ms 自动 `api.saveBin(path, png)`** → `POST /api/workspace/save-bin` → 原地覆盖同一个 tmp 文件（Ctrl+S 立即写回，关 tab 时补写）。
所以画板产物全程只有一个文件、固定在 tmp、路径不变；「添加到对话」= 先写回再把 `{name, path}` 塞进输入框。

**③ 从上下文打开一个文件、再把它加回上下文**
时间线/输入框里的资源入口（imgload 缩略图、附件 chip、引用 chip、`file://` chip）→ `store.openFileAt(path)` → 抽屉资源页按扩展名路由（`registry.kindOf`）：文本系读 `GET /api/workspace/file?path=`，图片进画板。
资源页「添加到对话」→ 文本系塞 `store.pendingFileRef = {path, items}`（带标注片段时 items 非空）→ 输入框 chip。**这一步同样不复制/移动任何文件**，只是把路径放进输入框。

**④ 点发送**
`store.send` → `api.send` → `POST /api/sessions/:id/messages`，body = `{text, files:[{name,path}], refs:[{path, items}]}`。
后端 `ChatController.SendMessage` 逐个 `checkPath`（转绝对路径 + 存在性校验）→ 合成 `[]hooks.RefFile` → `Session.StartRun(..., WithRefFiles(refs))`。
`hooks/reference_file.go` 的 `OnStart` 在本轮用户输入前插一条 `role=user` 的 `<reference_file>{hint, refs:[{path, items:[{sel,note,from,to}]}]}</reference_file>` 记录——**文件本体不进上下文**，只给路径与（可选、截断的）片段文本，模型需要内容时自己 `read_file`。该记录随本轮落盘 `sessions/<id>/session.json`，前端 `buildBlocks` 按同一标签重建引用 chips。

**唯一的例外是图片**：工具产出 `<image_loaded path="…"/>` 标记（read_file 读图、`service/browser.go` 截图），由 ezloop `filetools` 的 `OnLoop` **按路径读盘**转成带 base64 的 `role=user` 图片消息（无视觉模型由 `visionguard` 剥图）。所以图片是"进上下文"的，文本永远只是引用。

### 4.3 其它要点

- AI 侧文件工具（read_file/write_file/edit_file）和画板/文本保存走同一套绝对路径读写，没有目录白名单（`WorkspaceController` 只校验路径有效性与存在性）
- 跨窗口（抽屉 → 独立窗口）搬运的是**路径快照**（`filePane.serialize`），不复制文件；回流后图片 tab 会重挂按盘重读
- 浏览器截图每次一个文件名，会累积在 `workspace/browser/`（目前没有清理策略）
