# Hook/Warp 封装原则与消息策略

ezharness 基于 ezloop 开发。**核心原则：所有内容封装到 hook 和 warp 内**——ezharness（及未来的宿主）只做装配（选哪些 hook、传什么参数），不实现机制本身。本文档记录封装范式、两个维度与消息策略，作为新增能力时的设计准绳。

## 一、封装范式（样板：read_file 图片链）

一个能力的完整封装是四步，全部闭在一个 hook 内：

```
① 工具输出标记          read_file 读图 → 工具结果带 <image_loaded path="…"/>（机器标记）
② hook 点转换           OnLoop 扫标记 → 插入持久化 user 消息（<image_loaded> 包裹标签 + Images base64）
③ 发事件                state.EmitEvent(filetools.image_loaded, paths) → SSE → 前端实时渲染
④ 历史重载同格式渲染     前端 buildBlocks 识别消息标签 → 与实时路径同款渲染
```

判断封装是否完整：**基线归它、检测归它、消息归它、事件归它**——改动这个能力只需碰一个文件。反例：逻辑的一半长在别的 hook 里（借道说话）。

## 二、两个维度

### 单会话维度（per-session hook）

每个 session `Assemble` 时 new 一批新 hook 实例，处理**无状态能力**（skill、mcp、文件系统等）：hook 自身无状态，状态是 session 的（历史、基线快照、Metadata）。session 有生命周期，hook 随它生灭。

- 基线快照（"session 初始时的 skill/mcp 清单"）属于 session，检测发生在 hook 点（轮首对比基线）
- LoopState.Metadata 是 Run 内共享通道（runOpts 写、hook 读），跨 hook 传数据走它，不用全局变量

### 用户全局维度（app 级资源，所有 session 共用）

面向用户交互的资源绑定在**同一份 service/hook**上，per-session 的工具都指向同一底层实例——所有会话看到一致的资源、都能操作，换代会话不重建：

- **终端（现状）**：`TerminalService` 是 app 级单例（`a.Term`），全部 session 的 `term_*` 工具绑同一资源，多路复用 WS 实时共见
- **浏览器（规划）**：同一模式——app 级 BrowserService + per-session 工具绑定 + 全局一致性

## 三、消息策略

上下文里的消息分两类（完整清单见 runtime 文档）：

- **每轮固定**：用户输入、`<agent_status>`（水位/时间快照）、`<end_reason>`（轮末收尾）——固定节奏的系统同步
- **按需插入**：`<resource_change>`（有资源变更才有，附变更后 available 清单）、`<reference_file>`（有引用才有）、`<image_loaded>`（读图才有）、`<context_trim>`（整理时才有）——事件驱动，无事件零消息

**按需原则**：新能力告知模型一律走按需消息（有事实才说话），不往固定消息里塞行。标签风格统一（`<tag>` 包裹结构化内容，行式清单用 `- ` 前缀）：resource_change / reference_file / image_loaded / context_trim 同款，前端按标签识别、实时与历史两条渲染路径同格式。

## 四、系统提醒的归位：remind hook（已实施）

remind hook 统一负责"系统 → 模型"的全部单向旁路消息，内部分三段（壳 + 各段独立模块，防大杂烩）：

```
remind hook（per-session；internal/hooks/remind.go + reschange.go）
  ├─ 快照段（每轮 OnStart）：<agent_status> 水位/时间快照 + status.snapshot 事件（右上角水位条）
  ├─ 变更段（每轮 OnStart，reschange.go）：skill/mcp 基线 diff
  │     有变化 → 按需插一条 <resource_change> user 消息（agent_status 之前，含变更后
  │     available_skill:/available_mcp: 完整清单行，模型不必读目录/配置文件发现资源）
  │     + resource.change 事件
  │     无变化 → 零消息；基线（ResSnapshot）持久化随 session，重启不重复报
  └─ 收尾段（每轮 OnEnd）：<end_reason> 轮次/时长/结束原因 + 记录 LastOutputAt
        OnEnd 须在 sessionstore 落盘前（装配顺序保持）
```

首轮只建基线不 diff；fork 不跑 startHooks，变更段/快照段天然不进分身。未来全局资源（浏览器）的变更告知作为新的变更段模块加入。

### 分界判据：hook 归 ezharness 还是 ezloop

**依赖 session 域状态的 hook 留 ezharness internal/hooks；纯工具、无 session 状态的 hook 下沉 ezloop ext/hook。**

| 归属 | hook | 依据 |
|---|---|---|
| ezharness（宿主域） | remind（基线 ResSnapshot 在 Store）、trim（水位/折叠档案）、sessionstore、reference_file、guard、trace、sysprompt | 需要 session 状态（基线/历史/水位/SysPrompt） |
| ezloop（可复用组件） | filetools（工具+图片链）、skilltool、mcp、approve、askuser、task、offload、contextfix | 纯领域工具，参数化注入即用 |

skilltool 已下沉（`ext/hook/skilltool`，技能目录布局知识随迁 `skill.DirOf`，`load_skill` 为工具 Invoke 闭环）；skill/mcp hook 不做状态变更检测——一旦检测就耦合宿主基线，丧失可下沉性（这正是变更检测归 remind 的原因）。

trim 与归档（compact）的摘要与记忆方案（四节结构化 + progress.md、固定三记忆文件合并重写）详见 **compaction.md**。

## 五、warp 原则

warp 是节点装饰器（纵向封装，管节点内部；hook 是横向切面，管节点前后）。两条铁律：

- **请求视图 vs 落盘历史**：visionguard 只改请求副本（剥图换文案，落盘不动，能力切换可逆）；filetools OnLoop 插的消息直接入史（历史即事实）。改哪一层取决于语义：**适配**走请求视图，**事实**入历史
- **执行视图 vs 入史参数**：toolarg 只展开执行时的参数，入史的 ToolCall.Args 与工具卡展示保持模型原文——执行优化不重写历史

warp 链顺序即语义（先注册在外层）：模型链 modeldump → modelretry → visionguard（兜底最内，retry 每次尝试都生效）；工具链 toolarg → limit → safetool。
