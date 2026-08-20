# ezharness 设计文档

> 版本：v0.1（原型阶段） · 2026-08-20
> 基于当前前端原型整理，指导后续业务开发。

## 1. 产品定位

ezharness 是基于 [ezloop](https://github.com/xuanlv2002/ezloop) 内核的产品级 Web harness：

- **形态**：Go 单二进制（`go:embed` 前端产物），启动即开原生窗口（WebView2），也支持浏览器访问（`EZHARNESS_NO_WINDOW=1` 回落纯 server）。
- **权限模型**：全权限设备 agent——启动即对本机全权（文件不限目录 + shell），在哪启动操作哪台设备。
- **心智模型**：harness 提供机制与管控，ezloop 提供引擎，平台守事件流契约。
- **存储**：不用数据库，**文件夹即存储**。

## 2. 核心架构原则

1. **前端纯渲染**：一切数据与默认值由后端下发，前端不造数据、不含业务逻辑。原型阶段各页 `cfg = null` 占位。
2. **配置记录与结构配置分离**（详见 §4）。
3. **桌面优先**：布局按 WebView 窗口设计，兼顾浏览器。

### 2.1 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go + gin，三层 MVC（controller → service → domain），tools/hooks/osfs/config 支撑包 |
| 前端 | Svelte 5（runes）+ Vite + TypeScript |
| 通信 | REST + SSE（事件流单向推送，决策回传 POST） |

### 2.2 后端分层（v0.3 定调）

- `controller`：gin handler 只做绑定/校验/响应。
- `service`：用例（agent/chat/session/topic/settings/memory/mcp/app）。
- `domain`：Session 聚合（并发状态机）+ Hub（单一活动会话）+ 事件帧映射，零 HTTP 依赖。
- 决策回传一律异步（`go func + select`），hook 阻塞在 channel 上，同步发送死锁。
- 工具集四件：`read_file / write_file / edit_file / bash`（bash 自建：shell 探测 bash→pwsh→powershell→cmd，非零退出码返回输出+[exit code N] 供模型自纠）。

## 3. 信息架构

### 3.1 整体布局

两列：菜单列（64px 折叠 / 176px 展开，图标+中文，可切换）+ 主页列。

菜单分组（分隔线 + 组标题）：

```
[logo 光子轨道（ezloop 同款）]

工作面
  对话        主工作区
  快应用      agent 产出的小工具

──────── harness ────────
  模型        主模型 + 能力槽
  记忆        三个记忆文件夹
  知识库      正式文档库
  MCP         外部工具服务器
  安全        审批策略

（弹性空隙）
  设置        应用结构配置
  [展开/收起]
```

- 右下角固定角标：`ezharness v{version} · powered by ezloop`（链接到 ezloop 仓库，版本读 `package.json`）。
- 控制台启动横幅：isometric1 等轴测 3D 字体，逐行青→紫渐变（`#0ea5e9 → #8b5cf6`，ez 系列 logo 同款色系）。

## 4. 配置架构（v0.4 定调）

**结构配置与配置记录分离；零配置可启动。**

```
应用根/                        ← exe 首次执行所在文件夹（EZHARNESS_ROOT 可覆盖，dev 用）
  ezharness.json              ← 结构配置：{ "port": 5260, "dataDir": "data" }
                                 缺失自动创建默认；损坏备份 .bak 后重建；config 包内 mutex
  data/                        ← 数据目录（一切配置记录与数据，chdir 至此）
    models.json               ← 模型配置记录：{ apiKey, model, baseUrl }
    settings.json             ← 行为设置：{ systemExtra, rotateThreshold, shell, …审批策略(待) }
    mcp.json                  ← MCP 服务器
    memory/                   ← 记忆三子文件夹（路径可配置，见 §5.3）
    uploads/                  ← 附件副本（发送时后端 copy，消息引用副本路径）
    stats.json                ← 跨会话生命体征：首启时间 / 累计 usage / 轮数（已服务天数与缓存命中率的来源）
    topics.json / sessions/ / …
```

- **零配置启动**：apiKey 空照常起服务，UI 横幅引导到设置页；`Send/Summarize` 预检返回 400「未配置 API Key」。
- **.env/dotenv 机制已废除**；环境变量仅保留 `EZHARNESS_ROOT`（dev 锚定应用根）。
- 配置记录文件的结构随后续数据建模独立演进。

## 5. 页面规格

### 5.1 对话页（`ChatView`）

布局：主列（聊天记录 780px 居中 + 底部固定输入框）+ 右列 232px（状态卡 + 通知栏）。

**状态卡（`StatusCard`）**：当前上下文（水位）、已服务天数、缓存命中率、累计用量、当前工具。

**通知栏（`NoticePanel`）**——agent 人机交互请求的集中呈现：
- 四类：审批（approve）/询问（ask）/规划（plan）/通知（info）。
- **内联完成**：批准/拒绝、输入回答、执行/否决。
- **跳转定位**：`target` 指向时间线位置（含 fork 卡内的审批块），点击滚动定位。
- pending 高亮（左侧蓝条+微蓝底）与 done（打勾置灰）；头部待处理计数。
- 数据源：SSE 决策请求帧（approve/askuser/taskplan request）。

**输入框（`InputBar`）**：
- 静息态浅灰底细边框；聚焦白底黑边+轻阴影浮起，上方渐隐遮罩（聊天记录优雅淡出），下方快捷键提示浮现。
- 文本垂直居中（line-height 24px 锚定）。
- 发送键旁：魔法画板入口（魔棒图标）。

**附件与拖拽（`ChatView` + `InputBar`）**：
- 整个对话页为拖拽热区（dragenter/leave 计数防抖），拖入浮现虚线覆盖层。
- 附件条：图片 32px 缩略图（hover 出画笔标，点击进画板编辑）、其他文件文档图标、可移除。
- **生命周期**：内存 File → 点发送 → 后端 copy 一份到 `data/uploads/` → 消息引用副本路径（与原文件解耦，改名防冲突）。

**魔法画板（`MagicBoard`）**——对象模型白板：
- 元素模型：笔迹/矩形/椭圆/箭头/文本存对象数组；静态层缓存离屏 buf + rAF 节流叠加活动元素。
- 工具：选择（拖动/Del 删除/双击改字）、画笔、白笔（覆盖擦除）、矩形、椭圆、箭头、文本；四色（黑/白/强调蓝/红）三档笔粗；撤销 40 步（`$state.snapshot` 摘代理）、Ctrl+Z、清空。
- 视图变换：滚轮缩放（锚定光标，0.1–8×）、空格/中键平移、适应窗口重置；贴图后画布逻辑尺寸=图片原始尺寸（等比 cap 3072）保导出分辨率。
- **所见即所得**：一切内容裁剪在画布边界线内——画布上看到的 = 导出的 = 发给 AI 的。
- 导出：离屏合成 PNG，编辑回填替换原附件、空白创作新增附件。
- AI 返回图片（业务阶段）同入口进画板改后再回填附件。

### 5.2 模型页（`ModelsView`）

**「主模型 + 能力槽」四类**：

| 槽位 | 语义 |
|---|---|
| main 主模型 | 驱动 agent 循环，可直接选用多模态模型 |
| vision 多模态 | 主模型无视觉能力时的图片识别转写兜底 |
| image 图片生成 | 主模型按需调用的文生图工具后端 |
| audio 声音生成 | 主模型按需调用的语音合成工具后端 |

- 每类**单选启用一个**；条目含 apiKey/model/baseUrl、用量（tokens）、花费。
- 图片处理规则：主模型有视觉 → 直接进上下文（无损）；无 → 走 vision 槽转写（有损但可用）。
- 落盘演进方向：`models.json` 扩展为多模型记录。

### 5.3 记忆页（`MemoryView`）

记忆 = `memory/` 下三个子文件夹，路径可在设置中配置：

| 区块 | 形态 | 内容 |
|---|---|---|
| 长期记忆 | 文件列表 | `harness.md` 索引文件**初始加载进上下文**（置顶、accent 徽章），其余文件 agent 按需 grep 检索 |
| 能力记忆 | skill 卡片 | 沉淀的 skill（名称/描述/启停 toggle），可新建 |
| 话题记忆 | 会话列表 | 历史 session 存档（标题/日期/消息数），可回顾、hover 删除 |

### 5.4 知识库（`KnowledgeView`）

llmwiki 方案的正式文档库（与记忆的区别：**成体系的文档，而非零散内容**）：

- 入口两个：用户上传 / agent 存入（条目带「agent 存入」标签）。
- 文件入库**自动索引 + 摘要**；条目：类型徽章、摘要行、索引状态点、大小/时间。
- 顶部搜索框（文档检索）。

### 5.5 快应用（`ToolsView`）

agent 生成的 html 等小工具，统一存放在一个文件夹（可配置）：

- 卡片网格（自适应 2–3 列）：代码图标、类型徽章、名称、描述两行、时间、启动按钮；hover 浮起。
- 空态为虚线引导卡（「让 agent 做一个」）。
- 命名由来：与内置工具（bash 等）、MCP 区分。

### 5.6 MCP（`McpView`）

外部工具服务器卡片网格：

- 每卡：连接状态（已连接黑底图标/未连接）、服务器名、传输徽章（http/stdio）+ 端点、工具数、启停 toggle（停用降透明度）。
- 数据源：`mcp.json`（热加载）；细粒度审批策略后续在本页按服务器配。

### 5.7 安全（`SecurityView`）

**对 agent 的操作管控**（区别于应用设置——这里全是 agent 域）。核心是四档审批策略：

| 档位 | 语义 |
|---|---|
| 每次审批 | 每次调用都弹审批 |
| 黑名单审批 | 名单**外**放行，命中名单才审批 |
| 白名单免审 | 名单**内**放行，其余审批 |
| 全部免审 | 完全放行 |

- 选黑/白名单档时，该工具下**展开名单配置**（chips 增删）：bash=命令或前缀、文件工具=路径前缀、mcp=工具名。
- `task`（fork 分身）**只两档**（审批/免审），分身继承主 agent 策略（用户拍板，不做分身工具管控）。
- 落盘：`settings.json`（与 rotateThreshold/shell 同域）；为后端 `needsApprove` 逻辑的 UI 化与扩展。
- 页面预留成长空间：后续路径权限、危险操作策略等归入此页。

### 5.8 设置（`SettingsView`）

应用结构配置（harness 本体，非 agent 设置）：

1. **服务端配置**：端口（变更需重启，换代机制：boot 计数 + health 轮询跳转）。
2. **数据与配置文件**：应用根及各文件路径一览（后端下发）。
3. **记忆文件夹**：长期记忆/能力记忆/话题记忆三路径（初始 `memory/` 三子文件夹，可独立指向）。

## 6. 数据模型（前端契约形态）

原型各页定义的后端下发结构（接口待业务开发接入，字段可随数据建模演进）：

```ts
// 设置
interface AppConfig {
  port: number
  paths: { label: string; file: string; path: string }[]
  memory: { longterm: string; skills: string; topics: string }
}

// 模型（main/vision/image/audio 四槽）
interface ModelEntry { id: string; name: string; provider: string; baseUrl: string; enabled: boolean; usage: { tokens: number; cost: number } }
interface ModelsConfig { main: ModelEntry[]; vision: ModelEntry[]; image: ModelEntry[]; audio: ModelEntry[] }

// 记忆
interface MemoryConfig {
  longterm: { dir: string; harnessMd: FileInfo | null; files: FileInfo[] }
  skills: { dir: string; items: SkillEntry[] }
  topics: { dir: string; items: TopicEntry[] }
}

// 知识库
interface DocEntry { id: string; name: string; kind: string; summary: string; size: number; mtime: string; indexed: boolean; source: 'user' | 'agent' }

// 快应用
interface ToolEntry { id: string; name: string; desc: string; kind: string; mtime: string }

// MCP
interface McpServer { id: string; name: string; transport: 'http' | 'stdio'; endpoint: string; tools: number; connected: boolean; enabled: boolean }

// 通知（对话页右列）
interface Notice { id: string; kind: 'approve' | 'ask' | 'plan' | 'info'; source: string; title: string; detail?: string; time: string; status: 'pending' | 'done'; resolution?: string; target?: string }

// 审批策略（安全页，落 settings.json）
type Level = 'ask' | 'black' | 'white' | 'auto'
interface ToolRule { tool: string; desc: string; level: Level; kind: 'command' | 'path' | 'tool'; list: string[]; noList?: boolean }
```

## 7. 视觉规范

- **色彩**：极简高对比黑白 + 单一强调色 `--accent: #2563eb`（辅助 `--accent-soft`）；ez 系列品牌渐变青→紫（`#0ea5e9 → #8b5cf6`，logo 与控制台横幅）。
- **字体**：UI 用 Inter/系统栈；数据（路径/命令/模型名）一律 mono。
- **动效**：克制。`ease-out cubic-bezier(0.16,1,0.3,1)`，240ms（入场）/ 160ms（交互）；hover 仅边框/背景/轻浮起。
- **形态偏好**：
  - **卡片网格**：快应用、MCP、skill（离散、可启停的单元）
  - **列表**：模型条目、长期记忆文件、话题、审批矩阵（连续、同构的信息行）
- **状态色语义**：pending/启用 = accent；禁用/弱化 = 降透明度；删除 = 红 `#c0392b` hover 显现。

## 8. 待业务接入清单

| 模块 | 待接入 |
|---|---|
| ~~对话页~~ | ✅ 批次1 已接入：SSE 归约（时间线含 fork/决策卡/工具块）、发送/取消、决策回传闭环、StatusCard 生命体征（stats.json） |
| 附件 | 发送时上传（multipart）→ `data/uploads/` 落盘 → 消息引用 |
| 画板 | AI 返回图片进画板（随图片生成槽） |
| ~~通知~~ | ✅ 批次1 已接入：SSE 决策帧驱动（含去重/过期）、内联回传、跳转定位（fork 请求带 source 标识） |
| 模型页 | models.json 多模型读写、Reassemble 热更、用量/花费统计 |
| 记忆页 | 三文件夹管理 API、harness.md 读取、skill 启停、话题删除 |
| 知识库 | 上传、自动索引+摘要、检索 |
| 快应用 | 工具文件夹扫描、启动（webview/浏览器打开） |
| MCP | mcp.json 读写、连接状态探测、启停 |
| 安全 | 四档策略持久化（settings.json）+ 后端 needsApprove 改造 |
| 设置 | 端口/路径下发与修改（换代重启已有） |

## 9. 演进注记

- 模型四槽、记忆三文件夹、知识库、安全四档的结构都会随后续数据建模继续演进——当前为最小形态。
- 话题轮换（xhook rotate：压缩摘要→topics.json→轮内截断）等引擎侧机制见代码与项目记忆，不在本文档范围。
