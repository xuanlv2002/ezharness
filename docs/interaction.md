# 以人为核心的人机交互设计

agent 的能力边界再大，落到用户手里的也只是几个交互面。ezharness 把人机协同交互工具**外置**——不塞进对话流里硬凑，而是做成独立的产品面，让用户在 agent 工作时能看、能改、能答。

## 设计原则

1. **对话不是唯一的通道**。决策、观察、操作各有专门的界面；对话流只承载自然语言。
2. **通知全局，数据不绑视图**。无论用户停在哪个分支哪个页面，agent 需要人时一定能找到人。
3. **外置工具是"共享"的**。终端、（未来的）浏览器是人和 agent 共用的同一个实例，不是 agent 私有的副本——用户手敲的命令 agent 看得见，agent 的操作用户也看得见。

## 决策链路（approve / ask）

- 后端按 `toolRules.json` 四档判定（ask/black/white/auto，人机与内部工具恒免审），悬置时登记 pending（断线可重放）
- 前端 **DecisionCard**（approve/ask 两型）嵌在时间线；已决卡移除，结果以工具卡徽标呈现；刷新后由 `decisions.jsonl` 重建
- 决策回传 `POST /api/sessions/:id/decisions/approve|answer`；轮已结束的过期决策丢弃

## 全局通知栏（NoticePanel）

- 3s 轮询 `GET /api/notifications` 汇总**所有分支**（含后台分支与分身）的未决请求，全量替换（服务端为唯一真相，内容不变不换引用防抖动）
- 通知内联可直接决策（乐观移除，下轮轮询自洽）；「查看」跳转：跨分支先切线，分身请求开分身抽屉定位决策卡，主时间线锚点滚动
- 顺序稳定：后端双重排序（分支内按时间降序 + 组间排序）

## 共享终端（第一个外置协同工具）

设计定位：**人机共用同一个真实 shell**。agent 的 workDir 与终端一致，用户可以随时接管、演示、纠错。

- 后端 `core/internal/service/terminal.go`：Windows ConPTY 全局公共池（跨会话共享），环形缓冲 256KB（WS 重连恢复快照）
- `readMark` 单游标：term_send/term_read 共用，读即消费——agent 拿到的是用户上次阅读位置之后的新输出
- 用户手动输入聚合（转义序列过滤）进入 agent_status 的 Changes，agent 每轮知道用户在终端干了什么
- 前端 `lib/term.ts`：**单条 WebSocket 多路复用全部终端**（帧带 id 路由，hello/terminals 帧同步清单，指数退避重连）；工作区抽屉收起仅滑出，WS 与 xterm 常驻保活

## 路线图

同一模式——真实实例 + 单游标消费 + 人机双写——已落地两员（终端、浏览器），更多外置工具按此标准工程化：每个工具回答三个问题——人和 agent 各看到什么、写入如何仲裁、状态如何进入 agent_status。

## 共享浏览器（第二个外置协同工具，已实现）

同一模式搬到浏览器：**人机共用同一个 Chromium 实例**（`desktop/src/main/browser/index.js`）。

- 每个标签一个 `WebContentsView`（同一个 `partition: persist:ezbrowser`，登录态共享）；视图不挂 DOM——抽屉里的 `BrowserPane` 只画标签条与地址栏，内容区 rect 上报给主进程，主进程按 rect `setBounds` 贴上去，抽屉收起即上报零矩形让视图下线（`webContents` 存活，切回来秒开）
- 出处在桌面壳：core 侧 `browser_*` 工具经 `/api/browser/bridge`（WS，壳是客户端）转发给壳执行，结果回执进模型上下文；桥未连接时工具报「需要 desktop 桌面端」——web 端访问 core 时该工具自然不可用
- 标签可拖出成独立窗口（tear-off），视图随宿主窗口走

## 桌面壳（desktop/ · Electron 主进程）

- 拉 core：`spawn` `ezharness-core.exe` → 健康轮询 `/api/app/health` 就绪 → 无边框主窗口 `loadURL http://127.0.0.1:<port>/?desktop=1`（与浏览器访问同一份页面、同一个 core，同源，SSE/WS 都通）
- 托盘常驻：左键切换显示，右键菜单；关闭语义实时读设置——`closeToTray` 开 = 隐藏到托盘，关 = 前端页面 modal 询问（可勾选「以后最小化到托盘」持久化）
- 快应用子窗口（IPC `ez:open-app`）、抽屉工具的弹出窗口与**拖拽脱离**（`ez:popout-*`）
- `preload` 经 contextBridge 暴露 `window.ez`（窗口/弹出/浏览器三类能力）；web 端没有它，所有调用点判空降级

## 关键文件

`core/internal/service/terminal.go`、`core/internal/service/browser.go`、`frontend/src/lib/term.ts`、`frontend/src/components/board/`（`WorkspaceDrawer` · `TerminalTab` · `BrowserPane`）、`frontend/src/components/NoticePanel.svelte`、`frontend/src/components/DecisionCard.svelte`、`frontend/src/components/ForkPanel.svelte`、`desktop/src/main/index.js`、`desktop/src/main/browser/index.js`、`desktop/src/preload/index.js`
