# AGENTS.md — ezharness 开发备忘

变更时的连带检查清单与易踩的坑（按主题）。改动相关模块前先扫对应小节，
这里的每一条都是实际踩过的。

## agent_status / res_change（系统提醒注入）变更清单

remind hook（`internal/hooks/remind.go` + `reschange.go`）统一负责旁路提醒，
格式或文案改动牵一发动全身，以下全部要对齐：

- **生成**：`internal/hooks/remind.go` 的 `renderStatus`（中文语义化文本：当前时间 /
  上下文水位 / 距上次输出）；变更条目文案在 `reschange.go`（diffNames/diffTerms/用户操作）
- **历史重建识别**：`frontend/src/lib/store.svelte.ts` 的 `buildBlocks`——agent_status 按
  关键词 `整理上下文` 决定是否进时间线。**改 renderStatus 文案必须同步关键词**，
  否则消息被整个吞掉（建议整理被忽略）或普通轮次状态全量入时间线
- **状态卡渲染**：`frontend/src/components/StatusTagCard.svelte`——两条路径（旧 JSON
  `data` / 新中文文本 `raw`），新格式解析（当前时间/上下文水位）要与 renderStatus
  文案严格一致，否则历史重建时全文铺开（已修过一次）
- **`<res_change>` 契约**（变更段独立消息，按需插入）：`renderResChange` 条目行
  前缀 `- `；前端三处联动——`buildBlocks` 的 res_change 分支（matchAll
  `/^- (.+)$/gm`）、`ResChangeCard.svelte`（着色前缀：新增/用户绿、移除/已退出/
  已关闭红）、`store.apply` 的 `res.change` case（liveChanges + 插卡）；
  `res.change` 在 domain/session.go `replayable` 排除名单（回放重插会重复）
- **标题推导**：`FirstUserTitle` 按 `<agent_status>` / `<res_change>` / `<end_reason>`
  tag 跳过系统记录
- **基线**：`ResSnapshot{Skills, Mcps, Terms}` 每会话独立持久化（sessionstore），
  资源清单对比 = 全局实时清单 vs 本会话基线；首轮只建基线，用户终端操作
  收割即清不走基线
- **system 文案连带**：`agent_service.go` buildSystemBase 的 workspace 段
  文案描述终端/资源变更的呈现位置（"轮首 `<res_change>` 提示"）——改
  remind 呈现机制时必须同步，曾漏改导致 system 告诉模型错误信道（旧
  "每轮 agent_status"）；同理改 `<$supper_url>` / `<@toolArg>` 等模型
  侧语法时 workspace 段指引也要更新（且 system 每 session 固定，
  存量会话不回填，新文案只对新会话生效）

## 工具面增删/改名清单（term_run→term_send 的教训）

- `internal/service/agent_service.go`：**ToolNames 硬编码清单**；
  `needsApprove`/`matchRuleList` 的工具名匹配与命令词边界分支
- **工具实名以注册处为准**（如 ezloop filetools 的 `terminal`，tools.go:152），
  ToolNames 曾残留旧名 `bash` 导致右上角显示不存在 的工具——改名时
  grep 旧名逐一核对（settings.go LoadSettings 里的 bash→terminal 迁移是
  历史档兼容，属故意保留）
- `internal/domain/settings.go`：`DefaultToolRules` + `LoadSettings` 旧档迁移
  （参照 bash→terminal、term_run→term_send 的写法）。注意 **ToolRules 是全量覆盖
  语义**：用户档里没有的新工具默认 ask——不迁移就是能力倒退
- system prompt 工具指南（`buildSystemBase` 的 workspace 段文案）
- ezloop offload：`WithSkip` 名单（askuser / task / load_skill）
- 前端：grep 新旧工具名，确认 SettingsView / ToolGroup / api.ts 无硬编码残留

## skill 消费点（改 skill 相关须全查）

`skill.LoadDir` 共 **5 处**消费，每处都要按 `DisabledSkills` 过滤：
`settings_service.Config` / `agent_service.buildSystemBase` / `ezloop ext/hook/skilltool` /
`hooks/reschange.go` / `session_service.Status`。

- 技能身份用**目录名**（`skill.DirOf(s.Path)`，ezloop skill 包；ezharness
  `hooks.SkillDirOf` 是委托入口），frontmatter name 仅作显示名——
  开关/删除按目录名定位，别混用
- skilltool 已下沉 ezloop（`ext/hook/skilltool`，工具名常量 `skilltool.ToolName`）；
  hooks 不能 import service（会循环），禁用名单经闭包 `func() []string` 注入

## 终端模块

- **readMark 单游标**：`term_send` 与 `term_read` 共用 `TermSession.readMark`，
  send 返回增量后必须推进，否则 read 会重复输出
- 终端**全局共享**（个人助手语义）；agent_status 的终端基线每会话独立——
  A 会话首轮见到 B 会话开的终端报"新增"是**设计**（各会话模型知悉全局水位）
- WS 帧结构（`wsTermSessionOut`）改动要同步 `frontend/src/lib/term.ts` 的 `TermInfo`
- `killTree(nil)` 会 panic——手工构造 TermSession 的测试场景需 nil 防护（已加）

## SSE 时序（断线重连的坑）

- **回放帧语义**：SSE 建连时后端回放整轮聚合帧（`ReplayFrames`，含 loop_start/
  model_end/tool 卡），但前端 blocks 里可能已有半轮内容——**所有回放帧的 apply
  必须幂等**。曾踩坑：loop_start 去重只看"最后一个块"，重连时尾部已是模型输出
  → user 块重复 push（消息显示两次）
- **turn_end 不可回放**（`replayable` 名单排除）：轮在断线窗口内结束则前端永远
  收不到 → busy 卡死。修复：建连首帧 `replay.sync {turnActive}`（前端权威复位
  busy / 截断本轮块再收重放）+ `Cancel` 无运行轮时补发合成 turn_end
- **改帧类型/事件协议时**检查三处：后端 `Publish`/`replayable` 名单（session.go）、
  `MapEvent`（event.go）、前端 `apply` 的对应 case（含幂等性）

## 模型 warp 链

链序（先注册 = 外层）：`modeldump → modelretry → visionguard → provider`。
- 模型层错误**必须**在 provider 装饰器里处理（引擎在模型节点失败即终止
  loop，hook 拦截不到）
- `visionguard`（internal/warp/visionguard）：主模型 `ModelEntry.Vision=false`
  时每次实际请求（含 retry）把请求视图里全部 user Images 剥除、
  `<image_loaded>` 开标签注入 `omitted` 省略说明——**只改请求副本，落盘
  历史不动**（换回多模态图片自动恢复）。防"一次带图失败、图片留历史、
  之后每轮 400 卡死"
- 图片本体走 `<image_loaded>` user 消息持久化（read_file 标记 → filetools
  OnLoop 批末插入，详见 docs/hooks.md 封装范式）
- 图片路由说明**不进 system prompt**（buildSystemBase 不按 Vision 注入
  任何文案/目录行）：多模态模型图片直接进上下文无需说明，非多模态由
  omitted 说明自解释——曾因 system 烙下"你无法直接看图"残留，
  换多模态模型后（SysPrompt 每 session 固定，只复用不重建）误导模型怀疑
  自己的直接视觉

## 模型槽语义（四槽）

main 是唯一对话模型（Vision 开关决定图片进上下文还是落盘）；vision 槽
= 图片识别（启用 → 暴露 `image_recognize` 工具，service 层实时读槽配置
调 buildProvider，无需重建 agent）；image/audio 槽 = 预留（工具后续版本）。
改槽语义/工具暴露记得：ToolNames 动态清单、DefaultToolRules、ModelsView
槽文案三处同步。

## 构建/运行陷阱

- **三层结构**：`frontend/`（Svelte 双入口：index.html 主应用 +
  browser.html 浏览器窗口页）、`core/`（Go sidecar，module ezharness/core，
  前端产物构建时拷入 core/dist 后 go:embed）、`desktop/`（Electron 壳：
  主进程 + preload + electron-builder 配置）。业务全在 core，desktop 只做
  壳与桌面资产（窗口/托盘/内嵌浏览器）
- 构建：版本单源 `script/version.yaml`（脚本同步 frontend 页脚
  ezharness-v<version>/dev 与 desktop 打包版本，勿手改 package.json）；
  `script/dev.bat` 与 `script/release.bat` 同一条三段链——前端 → 拷
  core/dist → go build 出根目录 ezharness-core.exe → electron-builder；
  dev 出 `release/dev/ezharness-dev.exe`，release 出
  `release/v<version>/` 安装包 + 绿色版（细节见 docs/build.md）
- 应用根 = **core exe 所在目录**（config Root()，`--root` 参数可覆盖：
  portable 绿色版由 desktop 注入 PORTABLE_EXECUTABLE_DIR 后传参，数据
  跟随 exe）：
  ezharness.json 与 data/ 就地生成。开发时 exe 构建到仓库根（dev 也如此）；
  打包后 core 在 resources/，配置数据落在 resources/（安装版同规则）
- Electron 壳经 `process.resourcesPath`（打包）/仓库根（dev）找 core exe
  与 ezharness.json；改路径逻辑两处（desktop/src/main/index.js 的 coreDir
  与 spawn）要同步
- `go run ./core` 会把 exe 放 go-build 临时目录，config 按 exe 位置找不到
  ezharness.json → 回落默认端口 5260 + 空数据目录。**验证须
  `go build -o xxx.exe ./core` 后在配置所在目录运行**
- 进程 CWD = 数据目录（启动时 chdir），data 下文件用相对路径直接操作
- ezloop 是本地 replace（`../../ezloop`，core/go.mod），能不动就不动
- `fs.FileSystem` 接口无删除能力：删目录用 `os.RemoveAll`（service 层有 os 先例）
- 验证链：`cd core && go build ./... && go vet ./...` +
  `cd frontend && npm run build`；desktop 主进程 JS 用
  `node --check` 过一遍

## 共享浏览器（desktop 资产，桥架构）

- 真实浏览器 = Electron 主进程的 **WebContentsView**（每标签一个，
  `partition: persist:ezbrowser` 登录态共享）；浏览器窗口加载 core 伺服的
  /browser.html 只做 UI 框架（标签条/地址栏），内容区 rect 由 renderer
  ResizeObserver 经 IPC 上报、主进程 setBounds 贴靠
- AI 链路：core 的 browser_* 工具（定义/审批在 core，tools 层不感知实现）
  → `/api/browser/bridge` WS（JSON-RPC 式）→ desktop 执行器直接操作
  view.webContents（sendInputEvent/executeJavaScript/capturePage+CDP）。
  桥未连接（web 端直连 core）时工具报"仅桌面端"
- 窗口语义：AI start 自动弹出置前；用户关 X = **隐藏保留**（标签后台存活）；
  主应用地球钮/ browser:// chip 经 IPC 唤起定位
- 改 browser_* 工具签名时三处同步：core/internal/tools/browser.go（接口）、
  core/internal/service/browser.go（桥转发 params）、
  desktop/src/main/browser/index.js（executors）——桥协议字段名对齐

## 前端杂项

- API 错误形态是 `400: {"error":"xx"}`，展示给用户前用 errText 提取 error 字段
- 原生 `confirm()` 在桌面壳 WebView 里贴顶难看——确认弹窗用自制居中 modal
  （参考 MemoryView 的 skill 删除确认）
- desktop 能力经 preload `window.ez`（window.*/browser.*），web 端 undefined
  ——所有调用点判空降级（快应用/外链/浏览器入口/chip）
