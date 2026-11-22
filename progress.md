# 三分架构 · 分期开发进度

> 设计与分期总纲见 [docs/md/三分架构.md](docs/md/三分架构.md)。每期做完在此记录：改了什么、怎么验证。

## Phase 0 · 目录重划 — 已完成

frontend/ 整体迁入 core/（core 的内嵌 web 端，git mv 保留历史），构建链全部改路：

- `script/build.bat`、`dev.bat`、`dev.sh`、`sync-version.js`：frontend 路径 → core/frontend
- `.github/workflows/build.yml`：构建步骤路径
- `.gitignore`：删除两条死路径（裸 `node_modules`/`dist` 规则已覆盖）
- `core/embed.go` 注释、`docs/md/协同设计.md`、`docs/md/定时回调.md`：路径引用
- 新增 `docs/md/三分架构.md` 设计文档

验证：core/frontend `npm run build` ✓ → dist 拷入 core/web/dist ✓ → `go build` ✓

## Phase 1 · core 单实例地基 — 已完成

- **server.json**（应用根，无令牌）：`{port, pid, version, dataDir}`，core bind 成功后写入
  （`core/app.go` writeServerInfo），进程退出删除（clearServerInfo）；换代/格式化后按新端口重写。
  供入口程序（ezharness.exe/desktop）附着与探活。
- **版本注入**：`config.Version`（dev 构建 = "dev"），build.bat 与 CI 以
  `-X ezharness/core/internal/config.Version=<版本>` 注入；`/api/app/health` 回 `{ok, boot, version}`。
- **bind 仲裁**：端口 bind 成功者即主 core；抢不上时读 server.json 给出指路日志（可能已有实例）。
- **监听锁 127.0.0.1**：`config.Listen` 字段与 `normalizeListen` 全删（ezharness.json 的 listen 键
  自动被忽略），`ListenAddr(port)` 恒 127.0.0.1；设置页删除监听地址编辑，重启请求不再接受 listen。
- 测试：`go test ./internal/...` 全绿；冒烟（--root 临时目录）：health 带 version、server.json
  写入/退出清理、二次启动指路 ✓

## Phase 2 · 认证重构 — 已完成

- **core**（2a）：`service/auth.go`（auth.json：bcrypt 密码 + 多端持久会话）、
  `openapi_token.go` 重写（多枚命名令牌）、`audit.go`（/api/open/* jsonl 审计，5MB 轮转）、
  `controller/auth.go`（sessionAuth/openGuard 中间件、init/login/logout/sessions、
  安全页令牌与审计端点）；头名 EzHarnessToken / EzHarnessOpenToken；WS 子协议
  `["agent-auth", token]`（agent-auth 必须首位）；删 TokenStore、EZHARNESS_TOKEN 注入、
  日志入口 URL、/api/app/token、/api/auth/cookie、openapi 单令牌端点
- **前端**（2b）：LoginView（未初始化→设密码引导 / 已初始化→登录），令牌入 localStorage；
  App 登录门 + ez-auth-expired 失效回登；fetch 补关注入 EzHarnessToken；安全页新增
  OpenAPI 令牌/登录会话/审计三区；设置页删旧令牌区；sdk.js 与 open-api 技能改新头名
- **desktop**（2c）：删令牌随机生成与 env 注入；会话令牌存应用根 session.json，页面登录后
  经 ez:auth-token IPC 交主进程；coreFetch/浏览器桥带 EzHarnessToken；桥未登录时 3s 轮询
- **docs**（2d）：重写 docs/md/认证与openapi.md 为新模型
- 验证：go test ./internal/... 全绿（含新 auth/openapi/audit/中间件通道测试）；真二进制冒烟
  （init→token+cookie、头/cookie 通道、错密码 401、多端登录、openapi 令牌调 open 面过鉴权、
  审计落盘、重启会话仍有效、logout 撤销、SPA/新头名伺服）✓；desktop 三文件 node --check ✓

## Phase 3 · CLI（ezharness.exe）— 已完成

新 Go module `cli/`（ezharness/cli，纯 core 的 HTTP 客户端，不 import core 内部包）：

- **入口**：无参信息卡（300ms 短探活，双击运行等回车不闪退）、`-v`、`help`
- **子命令**：`server`（前台跟随，信号转发）/ `server --detach`（后台常驻）、
  `status`（纯键值，退出码 0/1）、`stop`（SIGTERM/taskkill，等 server.json 消失）、
  `logs`（尾部 40 行回放 + 跟随，换文件自动切）、`web`、`desktop`、
  `chat "一句话"`（流式打印，工具一行动作名，Ctrl+C 取消本轮，409 跟随现有轮）、
  `tui`（bubbletea：左分支右会话 + 输入框；Tab 切焦点、↑↓ 切分支自动 activate +
  重载历史 + 重订阅 SSE；replay.sync 重建运行中轮；空话题自助开分支）
- **ensureCore**：读 server.json → 探活 → 附着/拉起（轮询就绪上限 10s）
- **认证**：凭证文件（用户配置目录 ezharness/session.json）；首启终端引导设密码
  或登录，EzHarnessToken 头；SSE 子协议不需要（HTTP 头直传）
- **构建链**：build.bat / dev.bat / dev.sh / CI 均产出 ezharness(.exe)；CI 三平台
  构建并附到 Release（linux/macos 无安装包时的直接可用入口）
- 验证：真机冒烟——detached 拉起/status/stop 生命周期（server.json 随停清理）、
  信息卡、管道密码 init+登录、chat 发送失败路径干净退出、logs 跟随 ✓；TUI 渲染
  需真终端，留人工验
- 备注：本机 ~/codeProject/go.work 已加入 cli module（workspace 文件，不进仓库）

## Phase 4 · desktop 去父化 + 安装器 — 已完成

- **去父化**（desktop/src/main/index.js）：ensureCore 附着优先（读 server.json 定
  端口 → health → 已在跑直接附着，可能是 ezharness server 拉起的）；没跑则
  detached 拉起（stdio ignore + windowsHide），退出桌面壳不再杀 core（killCore
  删除，停止走 ezharness stop）；仅本进程拉起时保留崩溃检测（core 死 → 报错退出）。
  端口跟随：桥重连取 getPort()（core 换端口重启后桥自动跟上）。
- **打包布局**（electron-builder.yml）：executableName=ezharness-desktop（应用 exe
  改名避让 CLI 的 ezharness.exe；productName 不动——它决定 userData 目录名，改了
  丢老数据与密码）；core 与 cli 经 extraFiles 落安装根目录，三 exe 并排一条 PATH；
  产物名 ezharness-desktop-v*；coreDir() 改为应用 exe 同目录。
- **NSIS**（installer.nsh）：装完把 $INSTDIR 追加进 HKCU Environment\Path（标记位
  判重：同目录升级只写一次，换目录才追加；不做卸载清理——悬空项无害），
  广播 WM_SETTINGCHANGE；旧数据迁移宏保留。
- 验证：desktop dev 实测——附着既有 core ✓、无 core 时 detached 拉起 ✓、
  **桌面壳 kill -9 后 core 存活** ✓、ezharness stop 干净停 ✓；交叉打包 unpacked
  布局三 exe 并排 ✓（NSIS 脚本编译待 Windows/CI 首次构建验证）

## Phase 5 · 浏览器双后端 — 已完成

- **门面路由**（service/browser.go）：桥连接时走桥（桌面端真实浏览器 + 抽屉共见），
  否则走无头后端（`hd()` 判定，桥在连期间不切后端——标签属于各自后端）；
  截图落盘/image_loaded 标记抽成 `saveShots` 两后端共用。
- **无头后端**（service/browser_headless.go + browser_headless_scripts.go）：
  chromedp 驱动本机 Chrome/Edge/Chromium（headless、临时 profile、1280×900），
  探测顺序 macOS 应用目录 → Windows 标准安装路径 → PATH 命令；探测失败报
  agent 可读的安装指引（winget/brew/发行版）。能力面与桥对齐：start/navigate/
  read(text·html·elements·element·links·frames)/eval(深查询前置)/console/network/
  screenshot(viewport·fullPage 分段≤5·元素级)/upload(DOM.setFileInputFiles)/
  list/activate/close，标签状态 JSON 同构（含 output/note）。
- **核心坑与绕行**：chromedp v0.13.6/7 取消「跑过 Run 的派生上下文」会让该标签
  CDP 会话失去响应（纯 chromedp 复现确认）——`run()` 改为超时只放弃等待、
  不取消底层运行，命令按标签串行（sem）；父上下文首次 Run 预热以分配浏览器；
  静音 Chrome 新版枚举触发的 "could not unmarshal event" 良性日志。
- **生命周期**：core 退出/格式化时停浏览器并清临时 profile。
- 验证：`TestHeadlessBrowserFlow` 集成测试（本机有浏览器才跑）覆盖开标签→读正文→
  elements→eval 改 DOM→视口/元素截图→控制台采集→清单/快照→关标签 ✓；
  全量 `go test ./internal/...` 绿 ✓

## Phase 6 · 收尾（docs/CI/README）— 已完成

- README.md / README.en.md：架构段改为三 exe + 常驻 core（mermaid 重画，含
  浏览器桥/无头兜底、登录说明），文档索引加「三分架构」
- docs/index.html（在线文档站）：三分架构入网格/标签/DOCS 映射（01）
- docs/md/mcp设计.md：OpenAPI 头名改 EzHarnessOpenToken
- docs/md/三分架构.md：新增「远程形态（预留，当前不做）」——远程只有对话/模型/
  多媒体/MCP 可用，快应用与终端是本地回环语义，不做代理、如实标注部分可用
- CI：cli 构建落 release/dev/ezharness.exe（desktop extraFiles 按名取），另拷
  平台原生名（win 为 .exe）作直接下载入口；checksums.js 纳入裸名 ezharness
- 全量验证：core `go test ./internal/...` 绿、前端 build ✓、cli build ✓、
  安装器 unpacked 布局三 exe 并排（Windows/NSIS 编译由 CI 首次构建验证）

## Phase 7 · 全线复查修正 — 已完成

对 9903d85..69e58ae 全部改动逐个复查（样式/架构/正确性/AGENTS 规范/CI/本地脚本/三平台），
修正：

- **CLI 三平台**：`proc_win.go` 用了 Go `syscall` 不导出的 `DETACHED_PROCESS`，Windows 下
  编译失败（CI 会卡在 Build cli）——改为本地常量；`server --detach` 先 spawn 再
  ensureCore 会双起进程，改为只走 ensureCore；CI 的 Go 版本改为 1.26（cli 依赖要求）。
- **TUI**：`Init` 没启动 `waitForFrame`，SSE 帧从未被消费（首次发送后永远卡在"运行中"）；
  `sseStream` 的 done 通道被 stop 与 goroutine 双重 close（切分支可能 panic）、且 stop
  不取消请求——改 context 取消；用户消息本地回显与 loop_start 重放去重；SSE 错误经帧
  通道回主循环（消数据竞争）。
- **chat**：未过滤后台分身帧（输出串台、分身的 turn_end 会提前结束命令）。
- **认证**：OpenAPI 审计把被拒的 401 记成 200（先审计后写响应）——改顺序并加测试；
  cookie 只在只读 HTTP 请求（GET/HEAD）生效、WS 握手不接受 cookie（挡同 host 其他
  端口页面的 CSRF：cookie 不看端口，SameSite 也挡不住跨端口；WS 连上能发命令，
  浏览器侧必须走子协议令牌）；openapi 面保持原样；init/login 增加本机 Origin 校验
  （跨站页面不能抢先设密码，防 DNS 重绑定）。
- **前端**：SSE 仍用旧头名 X-EZ-Token（靠 cookie 才碰巧可用）——改 EzHarnessToken；
  登录门要求本页存在显式令牌（换端口后只剩 cookie 时直接回登录，而非半可用）；
  删掉重启跳转换端口时的 `?token=` 死代码（令牌不进 URL）。
- **desktop**：mac 的 extraFiles 落 `.app/Contents/` 而 exe 在 `Contents/MacOS`，coreDir
  之前找不到 core——按平台取父目录；端口跟随真正落地（getPort/pageBase/coreFetch 现读
  server.json，core 换端口重启后桥与新窗口自动跟上）；NSIS 安装/卸载前先停常驻 core
  （运行中的 exe 锁文件，否则覆盖/删除失败）。
- **无头浏览器**：桥接管时关掉无头实例（两套标签同用 b1/b2 编号，重连后会指错页面）；
  标签初始化失败时从清单摘除；控制台补未捕获异常与 Log 条目；网络明细补请求/响应头与
  响应体（与桥的能力面真正对齐），集成测试覆盖。
- **三平台/脚本**：CLI 交叉编译 linux/windows/darwin 均通过；CI 把 core 与 CLI 一起
  发布（linux/mac 无安装包时，裸下载的两个文件放同一目录即可 `ezharness server`）；
  build.bat 头部产物名、README 构建前置（Go 1.26+）与产物说明更新；
  `ezharness logs` 日志文件被截断时不再 panic。
- 文档同步：认证页免鉴权清单补 `/apps/*`、cookie 只读约束、init/login 的 Origin 限制；
  三分架构的 server.json/ensureCore 描述改为与实现一致。
- 复查补漏：审计清单空时回 `[]` 而非 null（安全页 `.length` 会炸）；登录页补齐
  表单样式（`.field/.primary/.err` 原本没有样式、`--ink2` 不存在）；页面启动时把
  已有令牌同步给桌面壳（session.json 丢失时不至于永远走无头）；README 中英安装段
  对齐三 exe / 产物名 / 纯 server 形态；CLI 信息卡在 Windows 双击时真正暂停、
  非终端密码读取保留空格；后台 core（Windows）用隐藏控制台启动；新增 CLI 单测
  （SSE 停止幂等/断开、TUI 帧合并）。

验证：core `go test ./internal/...` 全绿（含扩展后的无头集成测试）、
`go test -race`（controller/service/cli）全绿、前端 build ✓、cli 三平台交叉编译 ✓、
真机冒烟（init 403/登录、cookie 只读、审计 401、server/status/stop 生命周期）✓

## Phase 8 · 登录地址与安全页凭证 — 已完成

- **会话来源**：`Session` 增加 `ip`；init/login 记录 socket 地址（`RemoteIP`，
  `::1` 归一为 `127.0.0.1`），`GET /api/auth/sessions` 返回；旧会话无此字段时
  安全页显示"来源未知"。
- **安全页样式**：审批分组补可见说明与鼠标悬停；OpenAPI 令牌/登录会话/审计
  改为带分隔线的行式面板——令牌行显示名称、掩码与创建时间；会话行显示客户端
  类型（桌面端/浏览器/终端）、会话 id、来源 IP、登录时间；审计行显示方法、路径、
  令牌名、时间与状态（4xx/5xx 红色）。输入框与主按钮统一补样式。
- **desktop 登录地址**：登录页主窗增加服务器输入（默认当前 core，回环显示
  `localhost:5260`；web/宠物/通知/弹出窗不显示）。切换前经 IPC 探活目标 core，
  成功才改 base 并存 `desktop.json`，页面自行跳转；`coreFetch`、共享浏览器桥、
  快应用窗口统一走当前 base；切服务器清旧令牌并销毁挂在旧 core 上的附属窗。
  启动时记住的地址不可达自动回落本机 core 并清掉记录。顺手删除 before-quit 里
  已不存在的 `killCore()` 调用。
- **cli 登录地址**：`chat`/`tui` 支持 `--server <主机:端口>`；交互登录时先问
  服务器地址（回车用默认），管道/脚本不提问；`session.json` 增加 server 字段，
  换服务器自动要求重新登录；记住的远程不可达回落本机。TUI 标题显示实际地址。
- **文档**：认证与openapi、三分架构、README 中英补登录地址与会话来源说明。

验证：core `go test -count=1 ./internal/...` 全绿、cli `go test -count=1 ./...`
与 `go vet` 全绿、cli 三平台交叉编译 ✓、前端 build ✓、desktop 三个 JS
`node --check` ✓；`--server` 指向死端口返回"core 不可达"且不拉起本机 core ✓。
desktop 切换地址、Windows/NSIS 安装包留真机/CI 验证。

## Phase 9 · 默认密码与隐式登录 — 已完成

- **core**：启动时确保 `auth.json` 内是默认密码 `ezharness` 的 bcrypt 哈希
  （缺失/损坏/旧自定义密码则重建并清空旧会话）；删除 `/api/auth/init` 与
  `/api/auth/status` 及初始化引导，登录端点保留给三端静默换令牌；会话 cookie
  改为一年持久，浏览器重启后图片/PDF/视频仍可加载。
- **web**：删除 LoginView 与 App 登录门；`main.ts` 挂载前校验本地令牌，缺失或
  失效时用默认密码静默登录；`ez-auth-expired` 改为静默重登后刷新页面。
- **desktop**：移除登录页服务器切换残留（`desktop.json`、`ez:server-*` IPC、
  preload `server`、`reconnectBridge`）；固定跟随本机 `server.json`，令牌仍由
  页面登录后经 `ez:auth-token` 同步给主进程。
- **cli**：`chat`/`tui` 不再询问密码或地址，默认密码静默登录；`--server` 仍可
  显式指定 core；`session.json` 只存令牌；`go mod tidy` 移除不再直接使用的
  `golang.org/x/term`。
- **文档**：README 中英、[三分架构](docs/md/三分架构.md)、
  [认证与 OpenAPI](docs/md/认证与openapi.md) 同步默认密码与隐式登录。
- **CI**：新增 `test` job（前端构建、core/cli 测试与 vet、默认登录冒烟），
  三平台 `build` 依赖它；普通 push / PR 只跑测试，v* 标签或手动触发才出安装包。

验证：core `go test -count=1 ./internal/...` 与 `-race`（service/controller）全绿；
cli `go test -count=1 ./...`、`go vet`、三平台交叉编译 ✓；前端 `npm run build` ✓；
desktop 三个 JS `node --check` ✓。临时端口冒烟：init/status 404、默认密码登录 ✓、
错密码 401、auth.json 0600；headless Chrome 打开 web 端无登录页并自动生成 web
会话；cli `--server` 自动生成 cli 会话 ✓。

## Phase 10 · 安全页审批列表精修 — 已完成

- **行信息**：每条工具行显示一句用途说明；合并式工具（plan / callback / mcp 等）
  整行浅底作组头，展开后的逐 action 行缩进回白底。
- **控件**：分段控件从“大黑块”改为浅槽 + 淡蓝活动块；合并行和普通行统一显示
  「审批 / 免审」，不再一个宽一个窄，一键全审语义留在 tooltip 里。
- **修正**：`grep` 归回文件工具；`memory` / `model_call` 补说明并纳入稳定排序，
  不再缺描述且跑到系统自带组开头。

验证：前端 `npm run build` ✓；CDP 截图亮/暗主题、折叠/展开态 ✓。

## Phase 11 · OpenAPI 默认令牌 — 已完成

- **core**：`OpenapiStore.EnsureDefaultToken` 在启动/格式化后检查 `openapi.json`，
  空清单自动生成一枚名为「默认」的令牌并注入 `EZHARNESS_OPENAPI_TOKEN`；
  已有令牌不重复补。脚本、终端、快应用后端不再先让用户去安全页初始化。
- **安全页**：OpenAPI 令牌区文案改成“启动自动准备一枚‘默认’”，空态提示重启
  core 会补；仍支持多枚命名与吊销。
- **open-api 技能**：令牌获取改为默认令牌优先，只有 env/文件都没有时才让用户
  重启 core 或去安全页生成；顺手修正自检脚本读取 `openapi.json` 的字段
  （`tokens[-1].token`，原写法少了 `tokens`）。
- **文档 / CI**：认证与openapi、三分架构、README 中英补默认令牌说明；
  CI 冒烟新增默认令牌调 `/api/open/mcp` 校验。

验证：core 测试与 vet 全绿；临时端口冒烟确认空目录启动自动生成「默认」令牌、
令牌调 open 面 200、重启不重复；前端 build ✓。
