# 待办问题修复进度

逐个解决资源查看 / 侧栏 / 会话时间线的 7 个问题。每完成一项在此记录。

| # | 问题 | 状态 |
| --- | --- | --- |
| 1 | 资源查看·变更对比：滚动条旁加文件预览条（diff 缩略图），不用全量滚动找改动 | ✅ 已完成 | commit done
| 2 | 快应用 pin 后同时进左侧菜单栏（对话组与 harness 组之间），点击在 ezharness 打开 | ✅ 已完成 |commit done
| 3 | 对话内 file:// 超链接按文件类型展示（.jpg 缩略图等） | ✅ 已完成 | commit done
| 4 | 分支可收藏，置顶分支列表 | ✅ 已完成 | commit done
| 5 | 资源查看抽屉文件被外部刷新后内容不更新，且无刷新按钮 | ✅ 已完成 |commit done改为 VS Code 式自动跟盘（干净 tab 直接刷新，dirty 保存前确认覆盖），刷新钮已移除
| 6 | agent 调 context_trim 后，前面内容没有折叠起来 | ✅ 已完成 |  commit done
| 7 | 资源查看：markdown/html 引用的相对路径本地资源（图片等）显示失败 | ✅ 已完成 | 
| 8 | 快应用后端终端：分类呈现与联动（不另起服务管理器，避免重复基建与 agent 失明） | ✅ 已完成 |  commit done
| 10 | 工作目录文件乱丢：提示词加任务归档纪律（每任务一个 tasks/ 子目录），启动预建 tasks/ | ✅ 已完成 | commit done

| 11 | 多媒体模块：生成模型与语言模型配置分离（media.json）；生成改异步任务（提交+短等待+任务id，media_status 查询） | ✅ 已完成 |
| 9 | 对接 SiliconFlow 图片/TTS/视频三类生成模型：模型槽 + agent 工具 + OpenAPI，默认审批 + 每日配额 | ✅ 已完成 | 完全不行
## 记录

### 1. 变更对比概览轨（2026-11-15）

`frontend/src/components/viewer/viewers/DiffViewer.svelte`

- 正文右侧接管纵向滚动条的概览轨（14px，原生滚动条隐去）：半透明滑块即可视窗，悬停/拖动浮出浅槽底；轨道上只刻增/删行的细色条（上下文行不画）——一眼看出改动分布，长文件不用全量滚动找改动。
- 滚动位置按 `scrollTop/(scrollHeight-clientHeight)` 映射（滑块到顶到底严格对齐）；点在滑块内按抓取偏移拖动，点在轨道空白处跳到点按位置居中；`ResizeObserver` 跟容器尺寸重算。
- 迷你图版的 40px 大色块 + 上下文纹理已废弃（整条灰纹显脏、与项目卡片风格不搭）。

### 2. 钉住快应用进左侧菜单栏（2026-11-15）

`frontend/src/components/Sidebar.svelte`、`components/QuickAppView.svelte`（新增）、`lib/quickApp.svelte.ts`（新增）、`lib/apps.ts`、`components/AppLogo.svelte`（新增）

- 侧栏新增「钉住的快应用」独立块，位置在对话等工作组与 harness 配置组之间（`group-sep` 分隔）；数据与桌面小助手同一份 pin 名单（`GET /api/apps` 的 `pinned`，`api.pinApp` 共用），快应用页变动后广播 `ez:apps-changed` 即时刷新。
- **只有左侧钉住菜单在主窗口内嵌打开**：`openAppInside` 启动后端（如有）后切到 `app` 视图，`QuickAppView` 用 iframe 承载 `apps/<名>/<entry>`（同源 cookie 鉴权，dev 经 vite `/apps` 代理）。整页交给应用自己，壳上不叠标题/动作条（返回走侧栏导航，后端日志在终端页「快应用后端」分组）。工作区抽屉在 app 视图也上屏，应用与后端日志可同屏。
- **其余入口一律独立窗口**：快应用页只有一个「启动」键（卡片点击同义），`openAppWindow` 直接弹子窗；对话 `app://` chip、桌面宠物窗同语义，不再在主窗内嵌。卡片不再放第二个「独立窗口」键。
- **头像用应用自己的 logo**：`app.quick` 未声明 `icon` 时，在应用目录按序探测 `icon.svg/png`、`favicon.svg/ico/png`、`logo.svg/png`（声明优先）；都没有回落首字字母块——不再用闪电 glyph 占位。`AppLogo` 由侧栏 / 快应用卡 / 终端条目共用。

### 3. file:// 附件卡按文件类型特殊渲染（2026-11-15）

`frontend/src/components/MessageItem.svelte`、`lib/filemeta.ts`（新增）、`core/internal/service/workspace_service.go`、`controllers/workspace_controller.go`、`router.go`

- **图片卡**：整宽 240px 预览框（contain 不裁切）+ 文件名/尺寸角标（`onloadcapture` 补 `宽×高`），点击进资源页。
- **音/视频卡**：内嵌原生 `<audio>/<video controls preload=metadata>` 播放器（`loadedmetadata` 捕获补时长），播放器区域点击不触发打开动作；暗色主题下音频控件走 `color-scheme: dark`。
- **文档卡**：横向紧凑卡——左侧类型块（`PPTX`/`PY`/`PDF`…）+ 文件名 + 类型/页数/大小；新增 `GET /api/workspace/meta?path=` 轻量元数据（stat 大小 + pptx 从 zip 中央目录数幻灯片页数，不解压内容），前端 120ms 合并补料、同路径缓存一次。
- 附件卡统一项目风格（1px 边、圆角、主题变量），不再是迷你药丸；终端/快应用/浏览器的文本符号前缀（▤▶▣）也换成线框 SVG，克隆样式一致。
- 图片加载失败藏破图并在角标显示「图片无法预览」；点击仍走资源页按类型路由。

### 4. 分支收藏置顶（2026-11-15）

`frontend/src/components/BranchPanel.svelte`

- 分支行尾新增收藏钮（hover 浮现，与归档钮同规格）：收藏后金色（`fill` 实心）常亮，名单存 localStorage `ezh.branch.stars`（rootId 身份，compact 换代不换，收藏不掉）。
- 收藏的分支进置顶「收藏」组（组内仍按更新时间降序），不再进时间分组——重要分支不会被新增话题沉底；取消收藏回落原分组。
- **图标全部改用项目同款线框 SVG**：收藏星/归档/分叉徽标不再用 `★ ⇪ ⑂` 文本符号；对话里的分叉钮同步换成 SVG（「分叉」文字保留）。
- 行渲染抽成 Svelte 5 snippet 复用（收藏组与时间组同一份标记，不复制粘贴）。

### 5. 资源查看文件刷新（2026-11-15）

`frontend/src/components/viewer/ResourcePane.svelte`、`registry.ts`、`core/internal/controller/workspace_controller.go`

- **VS Code 式跟随磁盘**：资源页激活期间每 2s HEAD 轮询所有打开的文本 tab（text/md/html，不只当前 tab）。没有未保存修改的 tab 直接重载——agent/外部工具改完立即显示新内容，切回 tab 也是新的；手动刷新钮已移除。
- **磁盘指纹用 ETag**：文件服务按纳秒 mtime + size 生成 ETag（Last-Modified 只到秒，同一秒内的连续写入会被轮询漏掉）；前端优先比 ETag，老服务回退 Last-Modified。
- **dirty 标记与覆盖确认**：有未保存修改的 tab 保留本地内容、不被外部更新覆盖——tab 橙点 + 状态条「磁盘已被外部修改 · 保存将覆盖」；此时保存（含保存并关闭、添加到对话的自动保存）先弹确认（取消 / 覆盖保存），确认后才写盘。保存前会再 HEAD 比一次指纹，轮询间隔内的外部修改同样会弹窗（不会静默覆盖）。写成功后以新 ETag 为新基准，不会把自己的写入误报成外部更新。
- 打开/重载共用 2MB 只读门槛；重载请求返回时若用户已开始输入（内容不再等于已保存），放弃本次覆盖并转 dirty 保护。
- **浏览器页刷新钮**：地址栏右侧新增刷新钮（desktop 主进程新增 `ez-browser:reload` IPC）；AI 操作中的标签（亮锁）禁用，与地址栏导航同一把锁——浏览器 tab 不再只能靠切走再切回刷新。

### 6. context_trim 后折叠前面的内容（2026-11-15）

`lib/store.svelte.ts`、`lib/rows.ts`、`Timeline.svelte`、`ForkPanel.svelte`、`ThreadRows.svelte`

- 新增块类型 `trim`（分割线）：`<context_trim>` marker 历史重建与实时 `session.trim` 事件都从 note 升级为 trim 块，默认 `folded: true`。
- **摘要只做两件事**：概括当前内容 + 给出原文地址。trim 的 marker 直接带会话存档绝对路径（`session.json`，可读全文），不再写 progress.md 这类同内容副本；归档的 `<compact-summary>` 同样带上一会话 trace 绝对路径。需要细节时按地址读原文。
- **归档不再做记忆沉淀**：去掉 `distillToMemory` 这一步，归档只做摘要换代，不额外发记忆抽取模型请求；待确认记忆操作收窄为 memory 工具单一来源。以后需要记忆抽取再单独做。
- 新增 `foldBlocks()`（lib/rows）：每个折叠态 trim 块把「上一个 trim 之后 → 自己」之间的区块从渲染层裁掉（数据不删，多次整理各自独立折叠）；主时间线行组装前过一遍，分身抽屉展示完整记录不折叠。
- 整理标记 UI（ThreadRows）：单行信息条（1px 虚边 + 圆角 + SVG 行图标），折叠时前缀提示「向上滚动加载早期对话」，右侧摘要一行截断；点击头行原地展开整理全文（四节结构化摘要按小节排版，再点收起，与工具卡同一套展开交互）。不弹窗——时间线启用了 transform，fixed 弹层会被当成相对容器，出过遮罩留下、弹框跑偏的问题。trim 块新增 `full` 存摘要全文（历史从 marker 提取；实时整理由 `session.trim` 事件新增的 `summary` 字段携带），不再只有 160 字截断。
- 加载方式与全局懒加载一致（Timeline pull-to-load）：滚到顶继续上拉先载入折叠的早期对话（一次一批 + 行窗口限制，更早的继续走「显示更早」或再次上拉），载完再轮到上一话题；高度补偿保证视口不跳。分身抽屉展示完整记录，不做折叠。
- 轮次导航（turns）改用折叠后的块集——折叠区里的轮次不进导航（点了也滚不到）；空会话判定把 trim 也归为系统记录。
- 顺带修正：`session.trim`/`session.trimming` 带 forkId 时（分身整理）现在进分身聊天框，不再误挂到主时间线（否则分身的整理会把主对话折起来）。

### 7. markdown/html 相对路径本地资源解析（2026-11-15）

`lib/respaths.ts`（新增）、`viewers/MarkdownViewer.svelte`、`viewers/HtmlViewer.svelte`

- 新增 `resolveLocalRef(baseFile, ref)`：把相对引用（`img/x.png`、`../y/z.jpg`、`./a.svg`、`/abs/x.png`）按「文件所在目录」归一成绝对路径——`..` 不越根（POSIX 根 / Windows 盘符），锚点/查询串剔除，`\` 分隔符兼容；协议外链（http/data/mailto…）、协议相对（`//cdn`）、纯锚点返回 null 不改写。`fileResUrl()` 再转 `/api/workspace/file?path=…`（同源 cookie 鉴权，原有端点）。12 组解析用例全过。
- **MarkdownViewer**：消毒期经 DOMPurify `uponSanitizeAttribute` 钩子改写——媒体类标签（img/video/audio/source/embed/track/image）的 src → 文件服务 URL，`![](img/x.png)` 之类直接显示；指向本地文件的链接改为 `data-file-link` + 点击委托在资源页打开（不再让相对地址把整个 SPA 带跳 404）。原文编辑态不动，纯预览层行为。
- **HtmlViewer**：srcdoc 注入前按属性引法（双引号/单引号/裸值）正则改写 `src/href/poster/data`，沙箱 iframe 里本地图片、样式资源正常加载，本地链接在 iframe 内导航；沙箱禁脚本策略不变，无新增 XSS 面。

### 8. 快应用后端终端：分类呈现与联动（2026-11-15）

`core/internal/service/apps_service.go`、`lib/api.ts`、`components/ToolsView.svelte`、`components/board/TerminalTab.svelte`

**设计结论**（与用户确认后采用）：后端继续留在共享终端池，**不**另做独立服务管理器——另起一套要复制 PTY/WS/屏幕重放/AI 工具链，且 agent 反而看不见后端日志（现在 `term_read` 直接可读、人可接管 stdin，这是共享终端的核心价值）；架构耦合本来就低（apps_service 只依赖 `appTerm` 三方法接口 + `origin` 字符串标记「快应用·<名>」）。要解决的是**呈现层**：分类展示、关闭保护、应用卡联动。

- **后端**：`AppEntry` 新增 `backendLive` + `termId`（List 时从终端池按来源查活终端，`Term` 未装配安全跳过）。
- **终端抽屉分组**（TerminalTab）：strip 按 `origin` 分「工作终端」与「快应用后端」两组（两组并存才出「工作」标签）；分组标签用弱化灰、后端条目用应用自己的头像（`AppLogo`，缺省首字块）——去掉彩色 ⚡ emoji 与金色，不再和主题/项目图标风格打架；`ez:apps-changed` 时头像跟着刷新。
- **关闭保护**：活着的后端终端关闭弹框显式确认（「停止后端 / 取消」，说明应用将不可用），已退出的直接关——不做隐性二次点击。
- **应用卡联动**（ToolsView）：service 型卡片显示后端运行态（绿点呼吸=运行中 / 灰点=未运行），在跑可「查看终端」跳终端抽屉定位；启动后静默重拉名单点亮状态；页面打开期间每 15s 静默跟盘（不广播侧栏），后端崩了卡片自动变灰、再点「启动」即重启（现有复用逻辑只认活终端，天然支持）。卡片不再拉取/显示「N 个定时回调」——任务归回调中心，不在应用卡上反向聚合（应用自己声明的「端点：…」保留）。
- **查看终端修复**：抽屉只在对话 / 内嵌快应用页上屏，原先从快应用页点按钮只是拉开了 `termDrawerOpen`、页面没切过去，看起来无效；`openTermAt/openFileAt/openBrowserAt` 增加 `ensureWorkspaceView()`（不在工作区先 `jumpView='chat'`），顶栏工具键同步允许 `app` 视图。

### 9. SiliconFlow 三类生成模型接入（2026-11-15）

**架构：与语言模型完全同构**——模型配置复用 `ModelEntry`（baseUrl 填到 /v1 + apiKey + headers），models.json 新增第四槽 `video`（image/audio 槽位此前已预留、这次补齐真实工具）；`<models>` 系统段自动带出四槽清单。

**后端**（`service/media.go` 新增 MediaService、`tools/media.go` 三个工具、`controller/media_controller.go` OpenAPI）：

- **端点**（SiliconFlow 兼容）：文生图 `POST {base}/images/generations`（URL/b64 两形态产物，批量 1-4）；语音合成 `POST {base}/audio/speech`（预置音色短名自动补 `<model>:` 前缀，mp3/wav/opus/pcm，输入≤5000 字）；视频生成 `POST {base}/video/submissions` 提交 + `GET …/status` 轮询到出片（5-15 秒、480p/768p、可带首/尾帧=URL 或本机路径转 data URL），整体上限 10 分钟。
- **agent 工具**：`image_gen` / `audio_tts` / `video_gen`——能力槽**配置并启用才装配**（停用整槽即工具消失，与模型页既有承诺一致）；Description 内置成本纪律（张数=费用、非必要不 batch、视频先告知用户耗时）。
- **OpenAPI**：`POST /api/open/{image,audio,video}/call`（与 model/call 同级同鉴权），响应 `{ok, quota, files}`；图片/音频内嵌 base64，视频给原始 URL；SDK 新增 `ez.gen.image/speech/video`；open-api 技能文档同步。
- **产物**：统一落 `<工作目录>/tmp/gen-*`（与附件暂存同区，资源页可见、可回填对话），路径绝对正斜杠。

**限制（贵价模型两道闸）**：

1. **审批**：三个工具默认 `ask`（每次审批，`DefaultToolRules`），安全页可改；存量安装缺项自动回落默认。
2. **每日配额**：image 按张 / audio、video 按次，默认 20/20/3，设置页「生成限额」可调（0-1000）；agent 工具与 OpenAPI **同一份计数**（`mediaquota.json` 持久，跨日零点重置）；超限 OpenAPI 回 429 + 余量，工具结果报中文提示。**失败退还**：上游失败/提交失败/轮询 Failed/超时都不吃配额，图片按实际出图张数对账。

**前端**：模型页新增「视频生成」槽（含提示语：工具名与计费口径）、安全页三个工具说明行与排序、设置页「生成限额」区（三个输入 + 保存）；`api.ts` 补 `video` 槽 / 配额字段 / `genImage/genSpeech/genVideo`。

**测试**：`service/media_test.go`——配额（默认限额/超限哨兵/跨日清零/失败退还）、文生图（URL+b64 双形态、载荷、落盘路径、失败退额）、语音（音色前缀归一、超长拒绝）、视频（提交→InQueue→Succeed→下载、Failed 带原因退额）。Go 全量测试 + 前端 build/tsc 全绿。

### 10. 工作目录任务归档纪律（2026-11-15）

`core/internal/service/systemprompt.go`、`core/internal/service/layout.go`

**问题**：agent 每次跑完把草稿/脚本/产物全丢工作目录根部，任务一多就找不到东西。

- **`<workspace>` 路径纪律新增「任务归档」条**（路径纪律是唯一归属）：没有既定归属的新产物（草稿/脚本/生成文件/命令中间产物）统一放 `工作目录/tasks/<YYYYMMDD>-<任务英文短名>/`（如 `tasks/20261115-img-resize/`），动笔前先建目录再往里写；例外：用户给了路径、或本来就在改既有项目文件的按用户路径；terminal 重定向输出也进该目录（绝对路径）。
- **`<action>` 第 6 条交付准则同步**：产物按 `<workspace>` 任务归档放本任务目录、交付其入口（跨段只引用标签，不复述规则——遵循一事实一归属）。
- **`EnsureLayout` 预建 `tasks/`**：每次启动保证目录存在（与 tmp/、browser/ 同列），资源树里天然可见。
- 约束刻意保持一条（目录命名 + 时机 + 三个例外）：常驻 system prompt 每个字都是永久上下文，不展开成多级规范；任务内部再怎么组织交给模型自己判断。

### 11. 多媒体模块：配置分离 + 异步任务化（2026-11-15）

**动机**：生成模型混在语言模型体系里不优雅（models.json 槽位、`<models>` 系统段、model_call 取值域都有它们）；同步阻塞式工具让贵价生成卡住对话循环。

**配置分离**（语言模型 ⊥ 多媒体）：

- `models.json` 回归纯语言模型（`ModelsConfig{Main}`）；新增 `media.json`（`MediaConfig{Image,Audio,Video}`），Hub 各自持有快照。**启动自动迁移**：旧 models.json 的生成槽一次性搬进 media.json（按名去重、保留启停态），幂等。
- `<models>` 系统段与 model_call/task 取值域只剩对话模型——生成模型只经各自工具使用。模型页拆「主模型」+「多媒体生成」两组（双数据源 /api/models 与 /api/media，独立保存重建；多媒体编辑器隐藏协议/视觉/窗口字段，带 SiliconFlow 生成模型预设）。

**异步任务引擎**（`service/media.go` 重写）：

- `MediaJob{id,kind,status,files,quota,err,...}`：Submit* 校验+占额+入队即返回，后台 goroutine 执行（视频=上游异步提交→5s 轮询→下载成片）；任务档案持久 `mediajobs.json`（封顶 200 条终态），core 重启时未完成任务标记失败并退额（上游任务 id 已丢，v1 不续查）。
- 配额语义不变（提交占额、彻底失败退还、图片按实际张数对账），新增：部分成功（如 batch 4 出 3 张）按 done 交付 + 缺额退还。

**工具面**（agent 不再被贵价调用卡住）：

- `image_gen`/`audio_tts`/`video_gen`：提交后短等待 **15s**——快任务（图/音）直接拿产物路径；未完成返回任务 id + 节流指引（"不要空转轮询，隔开时间再查"）。
- 新增 `media_status{id?}`：查单个任务（产物/原因/进行中）或列最近 10 条（上下文整理后也能找回任务）；只读恒免审。

**OpenAPI/SDK**：

- `POST /api/open/{image,audio,video}/call` 增加 `wait_seconds`（默认 10、上限 60），响应 `{ok, job}`；新增 `GET /api/open/media/jobs[/:id]`。
- SDK：`ez.gen.image/speech/video` 返回 job，新增 `ez.gen.job(id)`/`ez.gen.jobs()`；open-api 技能文档同步异步语义与轮询纪律。

**测试**：任务回路（提交→短等待拿执行中快照→终态产物；Failed 退额；清单新→旧）、配额全套、**迁移幂等**；Go 全量 + 前端 build/tsc 全绿。

**后续钩子**（本轮未做）：MediaJob 结构已留 `CreatedAt/DoneAt`——事件回调（任务完成推送会话/通知）在 finishJob 处加一行发布即可。
